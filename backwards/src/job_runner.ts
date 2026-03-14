/// <reference path="../../packages/pocketci/src/global.d.ts" />

import {
  TaskAbort,
  TaskErrored,
  TaskFailure,
  TaskRunner,
} from "./task_runner.ts";
import { JobConcurrency } from "./job_concurrency.ts";
import {
  JobStoragePaths,
  zeroPad,
  zeroPadWithLength,
} from "./job_storage_paths.ts";
import { StepVariableResolver } from "./step_variable_resolver.ts";
import type { StepContext } from "./step_handlers/step_context.ts";
import {
  processGetStep,
  processPutStep,
} from "./step_handlers/resource_steps.ts";
import { processAgentStep } from "./step_handlers/agent_step.ts";
import {
  processDoStep,
  processParallelSteps,
  processTryStep,
} from "./step_handlers/composite_steps.ts";
import { processTaskStep } from "./step_handlers/task_step.ts";
import { processNotifyStep } from "./step_handlers/notify_step.ts";
import { processAcrossStep } from "./step_handlers/across_step.ts";

const buildID =
  (typeof pipelineContext !== "undefined" && pipelineContext.runID)
    ? pipelineContext.runID
    : zeroPad(Date.now(), 20);

export class JobRunner {
  private taskNames: string[] = [];
  private taskRunner: TaskRunner;
  private buildID: string;
  private paths: JobStoragePaths;
  private concurrency: JobConcurrency;
  private variableResolver: StepVariableResolver;
  private ctx: StepContext;

  constructor(
    private jobConfig: JobConfig,
    private resources: Resource[],
    private resourceTypes: ResourceType[],
    private pipelineMaxInFlight?: number,
  ) {
    this.buildID = buildID;
    this.taskRunner = new TaskRunner(this.taskNames, this.resources);
    this.paths = new JobStoragePaths(this.buildID, this.jobConfig.name);
    this.concurrency = new JobConcurrency(
      this.jobConfig.max_in_flight,
      this.pipelineMaxInFlight,
    );
    this.variableResolver = new StepVariableResolver();
    this.ctx = {
      paths: this.paths,
      concurrency: this.concurrency,
      variableResolver: this.variableResolver,
      taskRunner: this.taskRunner,
      resources: this.resources,
      resourceTypes: this.resourceTypes,
      buildID: this.buildID,
      jobName: this.jobConfig.name,
      processStep: (s, pc) => this.processStep(s, pc),
      runTask: (s, stdin, pc) => this.runTask(s, stdin, pc),
    };
  }

  async run(): Promise<void> {
    const storageKey = this.paths.getBaseStorageKey();
    let failure: unknown = undefined;
    const dependsOn = this.extractDependencies();

    const webhookFilter = this.jobConfig.triggers?.webhook?.filter ??
      this.jobConfig.webhook_trigger;

    if (webhookFilter) {
      if (!webhookTrigger(webhookFilter)) {
        storage.set(storageKey, { status: "skipped", dependsOn });
        return;
      }
    }

    const rawParams = this.jobConfig.triggers?.webhook?.params;
    if (rawParams) {
      this.variableResolver.setJobParams(webhookParams(rawParams));
    }

    storage.set(storageKey, { status: "pending", dependsOn });

    try {
      for (let i = 0; i < this.jobConfig.plan.length; i++) {
        await this.processStep(
          this.jobConfig.plan[i],
          zeroPadWithLength(i, this.jobConfig.plan.length),
        );
      }
      storage.set(storageKey, { status: "success", dependsOn });
    } catch (error) {
      console.error(error);
      failure = error;

      if (failure instanceof TaskFailure) {
        storage.set(storageKey, { status: "failure", dependsOn });
      } else if (failure instanceof TaskErrored) {
        storage.set(storageKey, { status: "error", dependsOn });
      } else if (failure instanceof TaskAbort) {
        storage.set(storageKey, { status: "abort", dependsOn });
      } else {
        storage.set(storageKey, { status: "error", dependsOn });
      }
    }

    try {
      if (failure === undefined && this.jobConfig.on_success) {
        await this.processStep(this.jobConfig.on_success, "hooks/on_success");
      } else if (failure instanceof TaskFailure && this.jobConfig.on_failure) {
        await this.processStep(this.jobConfig.on_failure, "hooks/on_failure");
      } else if (failure instanceof TaskErrored && this.jobConfig.on_error) {
        await this.processStep(this.jobConfig.on_error, "hooks/on_error");
      } else if (failure instanceof TaskAbort && this.jobConfig.on_abort) {
        await this.processStep(this.jobConfig.on_abort, "hooks/on_abort");
      }

      if (this.jobConfig.ensure) {
        await this.processStep(this.jobConfig.ensure, "hooks/ensure");
      }
    } catch (error) {
      console.error(error);
    }

    if (this.jobConfig.assert?.execution) {
      assert.equal(this.taskNames, this.jobConfig.assert.execution);
    }
  }

  private extractDependencies(): string[] {
    const dependencies: string[] = [];
    for (const step of this.jobConfig.plan) {
      if ("get" in step && step.passed) {
        for (const passedJob of step.passed) {
          if (!dependencies.includes(passedJob)) {
            dependencies.push(passedJob);
          }
        }
      }
    }
    return dependencies;
  }

  private async processStep(step: Step, pathContext: string): Promise<void> {
    const maxAttempts = step.attempts || 1;

    if (maxAttempts <= 1) {
      await this.processStepInternal(step, pathContext);
      return;
    }

    const { ensure, on_success, on_failure, on_error, on_abort, ...innerStep } =
      step as Step & StepHooks;

    let lastError: unknown = null;
    let succeeded = false;

    for (let attempt = 1; attempt <= maxAttempts; attempt++) {
      try {
        await this.processStepInternal(innerStep as Step, pathContext, attempt);
        succeeded = true;
        break;
      } catch (error) {
        lastError = error;
        if (attempt < maxAttempts) {
          console.log(`Attempt ${attempt}/${maxAttempts} failed, retrying...`);
        }
      }
    }

    try {
      if (succeeded && on_success) {
        await this.processStep(on_success, `${pathContext}/on_success`);
      } else if (!succeeded) {
        if (lastError instanceof TaskErrored && on_error) {
          await this.processStep(on_error, `${pathContext}/on_error`);
        } else if (lastError instanceof TaskAbort && on_abort) {
          await this.processStep(on_abort, `${pathContext}/on_abort`);
        } else if (lastError instanceof TaskFailure && on_failure) {
          await this.processStep(on_failure, `${pathContext}/on_failure`);
        }
      }
    } finally {
      if (ensure) {
        await this.processStep(ensure, `${pathContext}/ensure`);
      }
    }

    if (!succeeded && lastError) {
      throw lastError;
    }
  }

  private async processStepInternal(
    step: Step,
    pathContext: string,
    attempt?: number,
  ): Promise<void> {
    step = this.variableResolver.injectJobParams(step);

    if (step.across && step.across.length > 0) {
      await processAcrossStep(
        this.ctx,
        step,
        pathContext,
        (s, pc, a) => this.processStepInternal(s, pc, a),
      );
      return;
    }

    const resolvedPath = (s: Step) =>
      this.paths.withAttemptPath(
        `${pathContext}/${this.paths.getStepIdentifier(s)}`,
        attempt,
      );

    if ("get" in step) {
      await processGetStep(this.ctx, step, resolvedPath(step));
    } else if ("do" in step) {
      await processDoStep(this.ctx, step, resolvedPath(step));
    } else if ("put" in step) {
      await processPutStep(this.ctx, step, resolvedPath(step));
    } else if ("try" in step) {
      await processTryStep(this.ctx, step, resolvedPath(step));
    } else if ("task" in step) {
      await processTaskStep(this.ctx, step, resolvedPath(step));
    } else if ("in_parallel" in step) {
      await processParallelSteps(this.ctx, step, resolvedPath(step));
    } else if ("notify" in step) {
      await processNotifyStep(this.ctx, step, resolvedPath(step));
    } else if ("agent" in step) {
      await processAgentStep(this.ctx, step, resolvedPath(step));
    }
  }

  private async runTask(
    step: Task,
    stdin?: string,
    pathContext: string = "",
  ): Promise<RunTaskResult> {
    const storageKey = `${this.paths.getBaseStorageKey()}/${pathContext}`;
    let result: RunTaskResult;

    try {
      result = await this.taskRunner.runTask(step, stdin, storageKey);
    } catch (error) {
      if (step.on_error) {
        await this.processStep(step.on_error, `${pathContext}/on_error`);
      }

      throw new TaskErrored(
        `Task ${step.task} errored with message ${error}`,
      );
    }

    if (result.code === 0 && result.status == "complete" && step.on_success) {
      await this.processStep(step.on_success, `${pathContext}/on_success`);
    } else if (
      result.code !== 0 && result.status == "complete" && step.on_failure
    ) {
      await this.processStep(step.on_failure!, `${pathContext}/on_failure`);
    } else if (result.status == "abort" && step.on_abort) {
      await this.processStep(step.on_abort!, `${pathContext}/on_abort`);
    }

    if (step.ensure) {
      await this.processStep(step.ensure!, `${pathContext}/ensure`);
    }

    if (result.code > 0) {
      throw new TaskFailure(
        `Task ${step.task} failed with code ${result.code}`,
      );
    } else if (result.status == "abort") {
      throw new TaskAbort(
        `Task ${step.task} aborted with message ${result.message}`,
      );
    }

    return result;
  }
}

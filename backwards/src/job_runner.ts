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
import type { StepHandler } from "./step_handlers/step_handler.ts";
import { AcrossStepHandler } from "./step_handlers/across_step.ts";
import { AgentStepHandler } from "./step_handlers/agent_step.ts";
import { DoStepHandler } from "./step_handlers/do_step.ts";
import { GetStepHandler } from "./step_handlers/get_step.ts";
import { NotifyStepHandler } from "./step_handlers/notify_step.ts";
import { ParallelStepHandler } from "./step_handlers/parallel_step.ts";
import { PutStepHandler } from "./step_handlers/put_step.ts";
import { TaskStepHandler } from "./step_handlers/task_step.ts";
import { TryStepHandler } from "./step_handlers/try_step.ts";

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

  private acrossHandler = new AcrossStepHandler();
  private agentHandler = new AgentStepHandler();
  private doHandler = new DoStepHandler();
  private getStepHandler = new GetStepHandler();
  private notifyHandler = new NotifyStepHandler();
  private parallelHandler = new ParallelStepHandler(this.doHandler);
  private putHandler = new PutStepHandler();
  private taskHandler = new TaskStepHandler();
  private tryHandler = new TryStepHandler(this.doHandler);

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
      processStepInternal: (s, pc, a) => this.processStepInternal(s, pc, a),
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
      await this.acrossHandler.process(this.ctx, step, pathContext);
      return;
    }

    const handler = this.getHandler(step);
    if (handler) {
      const resolved = this.paths.withAttemptPath(
        `${pathContext}/${handler.getIdentifier(step)}`,
        attempt,
      );
      await handler.process(this.ctx, step, resolved);
    }
  }

  private getHandler(step: Step): StepHandler | undefined {
    if ("get" in step) return this.getStepHandler;
    if ("do" in step) return this.doHandler;
    if ("put" in step) return this.putHandler;
    if ("try" in step) return this.tryHandler;
    if ("task" in step) return this.taskHandler;
    if ("in_parallel" in step) return this.parallelHandler;
    if ("notify" in step) return this.notifyHandler;
    if ("agent" in step) return this.agentHandler;
    return undefined;
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

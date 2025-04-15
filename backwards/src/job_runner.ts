/// <reference path="../../packages/ci/src/global.d.ts" />

import {
  TaskAbort,
  TaskErrored,
  TaskFailure,
  TaskRunner,
} from "./task_runner.ts";

export class JobRunner {
  private taskNames: string[] = [];
  private taskRunner: TaskRunner;
  private buildID: number;

  constructor(
    private jobConfig: JobConfig,
    private resources: Resource[],
    private resourceTypes: ResourceType[],
  ) {
    this.buildID = Date.now();
    this.taskRunner = new TaskRunner(this.taskNames);
  }

  async run(): Promise<void> {
    const storageKey = this.getBaseStorageKey();
    let failure: unknown = undefined;

    storage.set(storageKey, { status: "pending" });

    try {
      for (let i = 0; i < this.jobConfig.plan.length; i++) {
        await this.processStep(this.jobConfig.plan[i], `${i}`);
      }
      storage.set(storageKey, { status: "success" });
    } catch (error) {
      console.error(error);
      failure = error;

      if (failure instanceof TaskFailure) {
        storage.set(storageKey, { status: "failure" });
      } else if (failure instanceof TaskErrored) {
        storage.set(storageKey, { status: "error" });
      } else if (failure instanceof TaskAbort) {
        storage.set(storageKey, { status: "abort" });
      } else {
        storage.set(storageKey, { status: "error" });
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

  private getBaseStorageKey(): string {
    return `/pipeline/${this.buildID}/jobs/${this.jobConfig.name}`;
  }

  private getStepIdentifier(step: Step): string {
    if ("task" in step) {
      return `tasks/${step.task}`;
    } else if ("get" in step) {
      return `get/${step.get}`;
    } else if ("put" in step) {
      return `put/${step.put}`;
    } else if ("do" in step) {
      return "do";
    } else if ("try" in step) {
      return "try";
    } else if ("in_parallel" in step) {
      return "in_parallel";
    }
    return "unknown";
  }

  private generateStorageKeyForStep(step: Step, currentPath: string): string {
    const basePath = this.getBaseStorageKey();
    const stepId = this.getStepIdentifier(step);
    return `${basePath}/${currentPath}/${stepId}`;
  }

  private async processStep(step: Step, pathContext: string): Promise<void> {
    if ("get" in step) {
      await this.processGetStep(
        step,
        `${pathContext}/${this.getStepIdentifier(step)}`,
      );
    } else if ("do" in step) {
      await this.processDoStep(
        step,
        `${pathContext}/${this.getStepIdentifier(step)}`,
      );
    } else if ("put" in step) {
      await this.processPutStep(
        step,
        `${pathContext}/${this.getStepIdentifier(step)}`,
      );
    } else if ("try" in step) {
      await this.processTryStep(
        step,
        `${pathContext}/${this.getStepIdentifier(step)}`,
      );
    } else if ("task" in step) {
      await this.processTaskStep(
        step,
        `${pathContext}/${this.getStepIdentifier(step)}`,
      );
    } else if ("in_parallel" in step) {
      await this.processParallelSteps(
        step,
        `${pathContext}/${this.getStepIdentifier(step)}`,
      );
    }
  }

  private async getFile(file: string, pathContext: string): Promise<string> {
    const mountName = file.split("/")[0];
    const result = await this.runTask(
      {
        task: `get-file-${file}`,
        config: {
          image_resource: {
            type: "registry-image",
            source: {
              repository: "busybox",
            },
          },
          inputs: [
            { name: mountName },
          ],
          run: {
            path: "sh",
            args: ["-c", `cat ${file}`],
          },
        },
        assert: {
          code: 0,
        },
      },
      undefined,
      pathContext,
    );

    return result.stdout;
  }

  private async processTaskStep(
    step: Task,
    pathContext: string,
  ): Promise<void> {
    if ("file" in step) {
      const contents = await this.getFile(step.file!, pathContext);
      const taskConfig = YAML.parse(contents) as TaskConfig;
      await this.runTask(
        {
          task: step.task,
          config: taskConfig,
          assert: step.assert,
          ensure: step.ensure,
          on_success: step.on_success,
          on_failure: step.on_failure,
          on_error: step.on_error,
          on_abort: step.on_abort,
          timeout: step.timeout,
        },
        undefined,
        pathContext,
      );
    } else {
      await this.runTask(step, undefined, pathContext);
    }
  }

  private async processParallelSteps(
    step: InParallel,
    pathContext: string,
  ): Promise<void> {
    await this.processDoStep(step, pathContext);
  }

  private async processTryStep(step: Try, pathContext: string): Promise<void> {
    try {
      await this.processDoStep(step, pathContext);
    } catch (_err) {
      // do nothing
    }
  }

  private async processDoStep(
    step: Do | Try | InParallel,
    pathContext: string,
  ): Promise<void> {
    let failure: unknown = undefined;

    try {
      let steps: Step[] = [];
      if ("in_parallel" in step) {
        steps = step.in_parallel.steps;
      } else if ("do" in step) {
        steps = step.do;
      } else if ("try" in step) {
        steps = step.try;
      }

      for (let i = 0; i < steps.length; i++) {
        const subStep = steps[i];
        await this.processStep(subStep, `${pathContext}/${i}`);
      }
    } catch (error) {
      failure = error;
    }

    if (failure == undefined && step.on_success) {
      await this.processStep(step.on_success, `${pathContext}/on_success`);
    } else if (failure instanceof TaskFailure && step.on_failure) {
      await this.processStep(step.on_failure, `${pathContext}/on_failure`);
    } else if (failure instanceof TaskErrored && step.on_error) {
      await this.processStep(step.on_error, `${pathContext}/on_error`);
    } else if (failure instanceof TaskAbort && step.on_abort) {
      await this.processStep(step.on_abort, `${pathContext}/on_abort`);
    }

    if (step.ensure) {
      await this.processStep(step.ensure, `${pathContext}/ensure`);
    }

    if (failure) {
      throw failure;
    }
  }

  private async processPutStep(step: Put, pathContext: string): Promise<void> {
    const resource = this.findResource(step.put);
    const resourceType = this.findResourceType(resource?.type);

    const putResponse = await this.runTask(
      {
        task: `put-${resource?.name}`,
        config: {
          image_resource: {
            type: "registry-image",
            source: {
              repository: resourceType?.source.repository!,
            },
          },
          outputs: [
            { name: resource?.name! },
          ],
          run: {
            path: "/opt/resource/out",
            args: [`./${resource?.name}`],
          },
        },
        assert: {
          code: 0,
        },
        ensure: step.ensure,
        on_success: step.on_success,
        on_failure: step.on_failure,
        on_error: step.on_error,
        on_abort: step.on_abort,
        timeout: step.timeout,
      },
      JSON.stringify({
        source: resource?.source,
        params: step.params,
      }),
      `${pathContext}/put`,
    );

    const putPayload = JSON.parse(putResponse.stdout);
    const version = putPayload.version;

    await this.runTask(
      {
        task: `get-${resource?.name}`,
        config: {
          image_resource: {
            type: "registry-image",
            source: {
              repository: resourceType?.source.repository!,
            },
          },
          outputs: [
            { name: resource?.name! },
          ],
          run: {
            path: "/opt/resource/in",
            args: [`./${resource?.name}`],
          },
        },
        assert: {
          code: 0,
        },
        ensure: step.ensure,
        on_success: step.on_success,
        on_failure: step.on_failure,
        on_error: step.on_error,
        on_abort: step.on_abort,
        timeout: step.timeout,
      },
      JSON.stringify({
        source: resource?.source,
        version: version,
      }),
      `${pathContext}/get`,
    );
  }

  private async processGetStep(step: Get, pathContext: string): Promise<void> {
    const resource = this.findResource(step.get);
    const resourceType = this.findResourceType(resource?.type);

    const checkResult = await this.runTask(
      {
        task: `check-${resource?.name}`,
        config: {
          image_resource: {
            type: "registry-image",
            source: {
              repository: resourceType?.source.repository!,
            },
          },
          run: {
            path: "/opt/resource/check",
          },
        },
        assert: {
          code: 0,
        },
        ensure: step.ensure,
        on_success: step.on_success,
        on_failure: step.on_failure,
        on_error: step.on_error,
        on_abort: step.on_abort,
        timeout: step.timeout,
      },
      JSON.stringify({
        source: resource?.source,
      }),
      `${pathContext}/check`,
    );

    const checkPayload = JSON.parse(checkResult.stdout);
    const version = checkPayload[0];

    await this.runTask(
      {
        task: `get-${resource?.name}`,
        config: {
          image_resource: {
            type: "registry-image",
            source: {
              repository: resourceType?.source.repository!,
            },
          },
          outputs: [
            { name: resource?.name! },
          ],
          run: {
            path: "/opt/resource/in",
            args: [`./${resource?.name}`],
          },
        },
        assert: {
          code: 0,
        },
        ensure: step.ensure,
        on_success: step.on_success,
        on_failure: step.on_failure,
        on_error: step.on_error,
        on_abort: step.on_abort,
        timeout: step.timeout,
      },
      JSON.stringify({
        source: resource?.source,
        version: version,
      }),
      `${pathContext}/get`,
    );
  }

  private findResource(resourceName: string) {
    const resource = this.resources.find((resource) =>
      resource.name === resourceName
    );
    return resource!;
  }

  private findResourceType(typeName?: string) {
    const resourceType = this.resourceTypes.find((type) =>
      type.name === typeName
    );
    return resourceType!;
  }

  private async runTask(
    step: Task,
    stdin?: string,
    pathContext: string = "",
  ): Promise<RunTaskResult> {
    // const storageKey = this.generateStorageKeyForStep(step, pathContext);
    const storageKey = `${this.getBaseStorageKey()}/${pathContext}`;
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

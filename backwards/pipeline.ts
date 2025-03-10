/// <reference path="../packages/ci/src/global.d.ts" />

type KnownMounts = {
  [key: string]: VolumeResult;
};

class PipelineRunner {
  private knownMounts: KnownMounts = {};
  private taskNames: string[] = [];

  constructor(private config: PipelineConfig) {
    this.validatePipelineConfig();
  }

  private validatePipelineConfig(): void {
    assert.truthy(
      this.config.jobs.length > 0,
      "Pipeline must have at least one job",
    );

    assert.truthy(
      this.config.jobs.every((job) => job.plan.length > 0),
      "Every job must have at least one step",
    );

    if (this.config.resources.length > 0) {
      this.validateResources();
    }
  }

  private validateResources(): void {
    assert.truthy(
      this.config.resources.every((resource) =>
        this.config.resource_types.some((type) => type.name === resource.type)
      ),
      "Every resource must have a valid resource type",
    );

    assert.truthy(
      this.config.jobs.every((job) =>
        job.plan.every((step) => {
          if ("get" in step) {
            return this.config.resources.some((resource) =>
              resource.name === step.get
            );
          }
          return true; // not a resource step, ignore lookup
        })
      ),
      "Every get must have a resource reference",
    );
  }

  async run(): Promise<void> {
    const job = this.config.jobs[0];
    for (const step of job.plan) {
      await this.processStep(step);
    }

    if (job.assert?.execution) {
      // this assures that the outputs are in the same order as the job
      assert.equal(this.taskNames, job.assert.execution);
    }
  }

  private async processStep(step: Step): Promise<void> {
    if ("get" in step) {
      await this.processGetStep(step);
    } else if ("do" in step) {
      await this.processDoStep(step);
    } else if ("put" in step) {
      await this.processPutStep(step);
    } else if ("try" in step) {
      await this.processTryStep(step);
    } else if ("task" in step) {
      await this.runTask(step);
    }
  }

  private async processTryStep(step: Try): Promise<void> {
    try {
      await this.processDoStep(step);
    } catch (_err) {
      // do nothing
    }
  }

  private async processDoStep(step: Do | Try): Promise<void> {
    let failure: unknown = undefined;

    try {
      const steps = "do" in step ? step.do : step.try;
      for (const subStep of steps) {
        await this.processStep(subStep);
      }
    } catch (error) {
      failure = error;
    }

    if (failure == undefined && step.on_success) {
      await this.processStep(step.on_success);
    } else if (failure instanceof TaskFailure && step.on_failure) {
      await this.processStep(step.on_failure);
    } else if (failure instanceof TaskErrored && step.on_error) {
      await this.processStep(step.on_error);
    }

    if (step.ensure) {
      await this.processStep(step.ensure);
    }

    if (failure) {
      // this only gets thrown if all others pass successfully
      throw failure;
    }
  }

  private async processPutStep(step: Put): Promise<void> {
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
      },
      JSON.stringify({
        source: resource?.source,
        params: step.params,
      }),
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
      },
      JSON.stringify({
        source: resource?.source,
        version: version,
      }),
    );
  }

  private async processGetStep(step: Get): Promise<void> {
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
      },
      JSON.stringify({
        source: resource?.source,
      }),
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
      },
      JSON.stringify({
        source: resource?.source,
        version: version,
      }),
    );
  }

  private findResource(resourceName: string) {
    const resource = this.config.resources.find((resource) =>
      resource.name === resourceName
    );
    return resource!;
  }

  private findResourceType(typeName?: string) {
    const resourceType = this.config.resource_types.find((type) =>
      type.name === typeName
    );
    return resourceType!;
  }

  private async runTask(step: Task, stdin?: string): Promise<RunTaskResult> {
    const mounts = await this.prepareMounts(step);

    const result = await runtime.run({
      name: step.task,
      image: step.config.image_resource.source.repository,
      command: [step.config.run.path].concat(step.config.run.args ?? []),
      mounts: mounts,
      stdin: stdin ?? "",
    });

    this.validateTaskResult(step, result);
    this.taskNames.push(step.task);

    if (result.code === 0 && result.status == "complete" && step.on_success) {
      await this.processStep(step.on_success);
    } else if (
      result.code !== 0 && result.status == "complete" && step.on_failure
    ) {
      await this.processStep(step.on_failure);
    } else if (result.status == "error" && step.on_error) {
      await this.processStep(step.on_error);
    }

    if (step.ensure) {
      await this.processStep(step.ensure);
    }

    if (result.code > 0) {
      throw new TaskFailure(
        `Task ${step.task} failed with code ${result.code}`,
      );
    }

    if (result.status == "error") {
      throw new TaskErrored(`Task ${step.task} errored`);
    }

    return result;
  }

  private async prepareMounts(step: Task): Promise<KnownMounts> {
    const mounts: KnownMounts = {};

    for (const mount of step.config.inputs ?? []) {
      this.knownMounts[mount.name] ||= await runtime.createVolume();
      mounts[mount.name] = this.knownMounts[mount.name];
    }

    for (const mount of step.config.outputs ?? []) {
      this.knownMounts[mount.name] ||= await runtime.createVolume();
      mounts[mount.name] = this.knownMounts[mount.name];
    }

    return mounts;
  }

  private validateTaskResult(step: Task, result: RunTaskResult): void {
    if (step.assert?.stdout && step.assert.stdout.trim() !== "") {
      assert.containsString(step.assert.stdout, result.stdout);
    }

    if (step.assert?.stderr && step.assert.stderr.trim() !== "") {
      assert.containsString(step.assert.stderr, result.stderr);
    }

    if (typeof step.assert?.code === "number") {
      assert.equal(step.assert.code, result.code);
    }
  }
}

class CustomError extends Error {
  constructor(message: string) {
    super(message);
    this.name = this.constructor.name;
  }
}

class TaskFailure extends CustomError {}
class TaskErrored extends CustomError {}

// Public API function
export function createPipeline(config: PipelineConfig) {
  const runner = new PipelineRunner(config);
  return () => runner.run();
}

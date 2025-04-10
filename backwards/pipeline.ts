/// <reference path="../packages/ci/src/global.d.ts" />

class PipelineRunner {
  private jobResults: Map<string, boolean> = new Map();
  private executedJobs: string[] = [];

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

    // Ensure job names are unique
    const jobNames = this.config.jobs.map((job) => job.name);
    assert.equal(
      jobNames.length,
      new Set(jobNames).size,
      "Job names must be unique",
    );

    // Validate that all passed constraints reference existing jobs
    if (this.config.jobs.length > 1) {
      this.validateJobDependencies();
    }

    if (this.config.resources.length > 0) {
      this.validateResources();
    }
  }

  private validateJobDependencies(): void {
    const jobNames = new Set(this.config.jobs.map((job) => job.name));

    // Check that all passed constraints reference existing jobs
    assert.truthy(
      this.config.jobs.every((job) =>
        job.plan.every((step) => {
          if ("get" in step && step.passed) {
            return step.passed.every((passedJob) => jobNames.has(passedJob));
          }
          return true;
        })
      ),
      "All passed constraints must reference existing jobs",
    );

    // Check for circular dependencies
    this.detectCircularDependencies();
  }

  private detectCircularDependencies(): void {
    // Build job dependency graph
    const graph: Record<string, string[]> = {};

    // Initialize empty adjacency lists
    for (const job of this.config.jobs) {
      graph[job.name] = [];
    }

    // Populate adjacency lists
    for (const job of this.config.jobs) {
      for (const step of job.plan) {
        if ("get" in step && step.passed) {
          for (const dependency of step.passed) {
            graph[dependency].push(job.name);
          }
        }
      }
    }

    // Check for cycles using DFS
    const visited = new Set<string>();
    const recStack = new Set<string>();

    const hasCycle = (node: string): boolean => {
      if (!visited.has(node)) {
        visited.add(node);
        recStack.add(node);

        for (const neighbor of graph[node]) {
          if (!visited.has(neighbor) && hasCycle(neighbor)) {
            return true;
          } else if (recStack.has(neighbor)) {
            return true;
          }
        }
      }

      recStack.delete(node);
      return false;
    };

    for (const job of this.config.jobs) {
      if (!visited.has(job.name) && hasCycle(job.name)) {
        assert.truthy(false, "Pipeline contains circular job dependencies");
      }
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
    // Find jobs with no dependencies
    const jobsWithNoDeps = this.findJobsWithNoDependencies();

    // Run jobs in dependency order
    for (const job of jobsWithNoDeps) {
      await this.runJob(job);
    }

    if (this.config.assert?.execution) {
      // this assures that the outputs are in the same order as the job
      assert.equal(this.executedJobs, this.config.assert.execution);
    }
  }

  private findJobsWithNoDependencies(): Job[] {
    return this.config.jobs.filter((job) => {
      return !job.plan.some((step) => {
        if ("get" in step && step.passed) {
          return true;
        }
        return false;
      });
    });
  }

  private async runJob(job: Job): Promise<void> {
    this.executedJobs.push(job.name);

    try {
      const jobRunner = new JobRunner(
        job,
        this.config.resources,
        this.config.resource_types,
      );
      await jobRunner.run();

      // Mark job as successful
      this.jobResults.set(job.name, true);

      // Find and run jobs that depend on this job
      await this.runDependentJobs(job.name);
    } catch (error) {
      // Mark job as failed
      this.jobResults.set(job.name, false);
      throw error;
    }
  }

  private async runDependentJobs(completedJobName: string): Promise<void> {
    const dependentJobs = this.findDependentJobs(completedJobName);

    for (const job of dependentJobs) {
      // Check if all dependencies are satisfied
      const canRun = this.canJobRun(job);
      if (canRun) {
        await this.runJob(job);
      }
    }
  }

  private findDependentJobs(jobName: string): Job[] {
    return this.config.jobs.filter((job) => {
      // Check if this job has a get step with a passed constraint including jobName
      return job.plan.some((step) => {
        if ("get" in step && step.passed && step.passed.includes(jobName)) {
          return true;
        }
        return false;
      });
    });
  }

  private canJobRun(job: Job): boolean {
    // Check if all passed constraints are satisfied
    for (const step of job.plan) {
      if ("get" in step && step.passed && step.passed.length > 0) {
        // Check if all jobs in passed constraint have completed successfully
        const allDependenciesMet = step.passed.every(
          (depJobName) => this.jobResults.get(depJobName) === true,
        );

        if (!allDependenciesMet) {
          return false;
        }
      }
    }

    return true;
  }
}

class JobRunner {
  private knownMounts: KnownMounts = {};
  private taskNames: string[] = [];

  constructor(
    private job: Job,
    private resources: Resource[],
    private resourceTypes: ResourceType[],
  ) {}

  async run(): Promise<void> {
    for (const step of this.job.plan) {
      await this.processStep(step);
    }

    if (this.job.assert?.execution) {
      // this assures that the outputs are in the same order as the job
      assert.equal(this.taskNames, this.job.assert.execution);
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
      await this.processTaskStep(step);
    } else if ("in_parallel" in step) {
      await this.processParallelSteps(step);
    }
  }

  private async getFile(file: string): Promise<string> {
    const mountName = file.split("/")[0];
    // check if mount exists
    if (!this.knownMounts[mountName]) {
      throw new Error(`Mount ${mountName} does not exist`);
    }

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
    );

    if (result.code !== 0) {
      throw new Error(`Failed to get file ${file}`);
    }

    return result.stdout;
  }

  private async processTaskStep(step: Task): Promise<void> {
    if ("file" in step) {
      const contents = await this.getFile(step.file!);
      const taskConfig = YAML.parse(contents) as TaskConfig;
      await this.runTask({
        task: step.task,
        config: taskConfig,
        assert: step.assert,
        ensure: step.ensure,
        on_success: step.on_success,
        on_failure: step.on_failure,
        on_error: step.on_error,
        on_abort: step.on_abort,
        timeout: step.timeout,
      });
    } else {
      await this.runTask(step);
    }
  }

  private async processParallelSteps(step: InParallel): Promise<void> {
    await this.processDoStep(step);
  }

  private async processTryStep(step: Try): Promise<void> {
    try {
      await this.processDoStep(step);
    } catch (_err) {
      // do nothing
    }
  }

  private async processDoStep(step: Do | Try | InParallel): Promise<void> {
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
    } else if (failure instanceof TaskAbort && step.on_abort) {
      await this.processStep(step.on_abort);
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
        timeout: step.timeout,
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
        timeout: step.timeout,
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
        timeout: step.timeout,
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
        timeout: step.timeout,
      },
      JSON.stringify({
        source: resource?.source,
        version: version,
      }),
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

  private async runTask(step: Task, stdin?: string): Promise<RunTaskResult> {
    const mounts = await this.prepareMounts(step);

    this.taskNames.push(step.task);

    let result: RunTaskResult;

    try {
      result = await runtime.run({
        command: {
          path: step.config.run.path,
          args: step.config.run.args || [],
          user: step.config.run.user,
        },
        env: step.config.env,
        image: step.config?.image_resource.source.repository!,
        name: step.task,
        mounts: mounts,
        privileged: step.privileged ?? false,
        stdin: stdin ?? "",
        timeout: step.timeout,
      });

      this.validateTaskResult(step, result);

      if (result.code === 0 && result.status == "complete" && step.on_success) {
        await this.processStep(step.on_success);
      } else if (
        result.code !== 0 && result.status == "complete" && step.on_failure
      ) {
        await this.processStep(step.on_failure);
      } else if (result.status == "abort" && step.on_abort) {
        await this.processStep(step.on_abort);
      }

      if (step.ensure) {
        await this.processStep(step.ensure);
      }
    } catch (error) {
      if (step.on_error) {
        await this.processStep(step.on_error);
      }

      throw new TaskErrored(
        `Task ${step.task} errored with message ${error}`,
      );
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

  private async prepareMounts(step: Task): Promise<KnownMounts> {
    const mounts: KnownMounts = {};

    const inputs = step.config.inputs || [];
    const outputs = step.config.outputs || [];

    for (const mount of inputs) {
      this.knownMounts[mount.name] ||= await runtime.createVolume();
      mounts[mount.name] = this.knownMounts[mount.name];
    }

    for (const mount of outputs) {
      this.knownMounts[mount.name] ||= await runtime.createVolume();
      mounts[mount.name] = this.knownMounts[mount.name];
    }

    return mounts;
  }

  private validateTaskResult(step: Task, result: RunTaskResult): void {
    if (step.assert?.stdout && step.assert.stdout.trim() !== "") {
      assert.containsString(result.stdout, step.assert.stdout);
    }

    if (step.assert?.stderr && step.assert.stderr.trim() !== "") {
      assert.containsString(result.stderr, step.assert.stderr);
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
class TaskAbort extends CustomError {}

// Public API function
export function createPipeline(config: PipelineConfig) {
  const runner = new PipelineRunner(config);
  return () => runner.run();
}

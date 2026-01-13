/// <reference path="../../packages/ci/src/global.d.ts" />

import {
  TaskAbort,
  TaskErrored,
  TaskFailure,
  TaskRunner,
} from "./task_runner.ts";

function zeroPad(num: number, places: number): string {
  return String(num).padStart(places, "0");
}

function zeroPadWithLength(num: number, length: number): string {
  const decimalPlaces = String(length).split(".")[1]?.length || 0;
  return zeroPad(num, decimalPlaces);
}

// Use pipelineContext.runID if available (from server), otherwise fall back to timestamp
const buildID =
  (typeof pipelineContext !== "undefined" && pipelineContext.runID)
    ? pipelineContext.runID
    : zeroPad(Date.now(), 20);

export class JobRunner {
  private taskNames: string[] = [];
  private taskRunner: TaskRunner;
  private buildID: string;

  constructor(
    private jobConfig: JobConfig,
    private resources: Resource[],
    private resourceTypes: ResourceType[],
  ) {
    this.buildID = buildID;
    this.taskRunner = new TaskRunner(this.taskNames);
  }

  async run(): Promise<void> {
    const storageKey = this.getBaseStorageKey();
    let failure: unknown = undefined;
    const dependsOn = this.extractDependencies();

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
    } else if ("notify" in step) {
      const name = Array.isArray(step.notify)
        ? step.notify.join("-")
        : step.notify;
      return `notify/${name}`;
    }
    return "unknown";
  }

  private generateStorageKeyForStep(step: Step, currentPath: string): string {
    const basePath = this.getBaseStorageKey();
    const stepId = this.getStepIdentifier(step);
    return `${basePath}/${currentPath}/${stepId}`;
  }

  private async processStep(step: Step, pathContext: string): Promise<void> {
    // Handle attempts wrapper - retry up to N times
    const maxAttempts = step.attempts || 1;

    for (let attempt = 1; attempt <= maxAttempts; attempt++) {
      try {
        await this.processStepInternal(step, pathContext);
        return; // Success - exit retry loop
      } catch (error) {
        // If we haven't reached max attempts, retry
        if (attempt < maxAttempts) {
          console.log(`Attempt ${attempt}/${maxAttempts} failed, retrying...`);
          continue;
        }
        // Max attempts reached, throw the error
        throw error;
      }
    }
  }

  private async processStepInternal(
    step: Step,
    pathContext: string,
  ): Promise<void> {
    // Handle across wrapper - run step multiple times with different variable combinations
    if (step.across && step.across.length > 0) {
      await this.processAcrossStep(step, pathContext);
      return;
    }

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
    } else if ("notify" in step) {
      await this.processNotifyStep(
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

  private async processAcrossStep(
    step: Step,
    pathContext: string,
  ): Promise<void> {
    // Generate all combinations of variable values
    const combinations = this.generateAcrossCombinations(step.across!);

    const storageKey = `${this.getBaseStorageKey()}/${pathContext}/across`;
    storage.set(storageKey, { status: "pending", total: combinations.length });

    let failureOccurred = false;
    const failFast = (step as { fail_fast?: boolean }).fail_fast || false;

    for (let i = 0; i < combinations.length; i++) {
      if (failureOccurred && failFast) {
        break; // Stop processing if fail_fast is enabled and a failure occurred
      }

      const combination = combinations[i];
      const varContext = Object.entries(combination)
        .map(([key, value]) => `${key}_${value}`)
        .join("_");

      // Create a modified step with the across variables injected as env vars
      const modifiedStep = this.injectAcrossVariables(step, combination);

      try {
        await this.processStepInternal(
          modifiedStep,
          `${pathContext}/across/${i}_${varContext}`,
        );
      } catch (error) {
        failureOccurred = true;
        if (failFast) {
          storage.set(storageKey, { status: "failure", failed_at: i });
          throw error;
        }
        // Continue processing other combinations if fail_fast is not enabled
        console.error(`Across combination ${i} failed:`, error);
      }
    }

    if (failureOccurred && !failFast) {
      storage.set(storageKey, { status: "failure" });
      throw new TaskFailure("One or more across combinations failed");
    }

    storage.set(storageKey, { status: "success", total: combinations.length });
  }

  private generateAcrossCombinations(
    acrossVars: AcrossVar[],
  ): Record<string, string>[] {
    // Generate cartesian product of all variable values
    if (acrossVars.length === 0) {
      return [{}];
    }

    const [first, ...rest] = acrossVars;
    const restCombinations = this.generateAcrossCombinations(rest);
    const combinations: Record<string, string>[] = [];

    for (const value of first.values) {
      for (const restCombination of restCombinations) {
        combinations.push({
          [first.var]: value,
          ...restCombination,
        });
      }
    }

    return combinations;
  }

  private injectAcrossVariables(
    step: Step,
    variables: Record<string, string>,
  ): Step {
    // Clone the step and inject across variables into task config env
    const clonedStep = { ...step };

    if ("task" in clonedStep && clonedStep.config) {
      clonedStep.config = {
        ...clonedStep.config,
        env: {
          ...clonedStep.config.env,
          ...variables,
        },
      };
    }

    // Remove across fields from cloned step to avoid infinite recursion
    delete (clonedStep as Record<string, unknown>).across;
    delete (clonedStep as Record<string, unknown>).fail_fast;

    return clonedStep;
  }

  private async processTryStep(step: Try, pathContext: string): Promise<void> {
    try {
      await this.processDoStep(step, pathContext);
    } catch (_err) {
      // do nothing
    } finally {
      // always successful
      storage.set(pathContext, { status: "success" });
    }
  }

  private async processNotifyStep(
    step: NotifyStep,
    pathContext: string,
  ): Promise<void> {
    const storageKey = `${this.getBaseStorageKey()}/${pathContext}`;
    let failure: unknown = undefined;

    try {
      storage.set(storageKey, { status: "pending" });

      // Update notify context with current job info
      notify.updateJobName(this.jobConfig.name);
      notify.updateStatus("running");

      // Send to single or multiple notification configs
      const names = Array.isArray(step.notify) ? step.notify : [step.notify];

      if (step.async) {
        // Fire-and-forget mode
        for (const name of names) {
          notify.send({ name, message: step.message, async: true });
        }
        storage.set(storageKey, { status: "success" });
      } else {
        // Synchronous mode - wait for result
        if (names.length === 1) {
          await notify.send({
            name: names[0],
            message: step.message,
            async: false,
          });
        } else {
          await notify.sendMultiple(names, step.message, false);
        }
        storage.set(storageKey, { status: "success" });
      }
    } catch (error) {
      failure = error;
      storage.set(storageKey, { status: "failure" });
    }

    // Handle step hooks
    if (failure === undefined && step.on_success) {
      await this.processStep(step.on_success, `${pathContext}/on_success`);
    } else if (failure && step.on_failure) {
      await this.processStep(step.on_failure, `${pathContext}/on_failure`);
    }

    if (step.ensure) {
      await this.processStep(step.ensure, `${pathContext}/ensure`);
    }

    if (failure) {
      throw new TaskFailure(`Notification failed: ${failure}`);
    }
  }

  private async processDoStep(
    step: Do | Try | InParallel,
    pathContext: string,
  ): Promise<void> {
    const storageKey = `${this.getBaseStorageKey()}/${pathContext}`;
    let failure: unknown = undefined;

    try {
      storage.set(storageKey, { status: "pending" });

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
        await this.processStep(
          subStep,
          `${pathContext}/${zeroPadWithLength(i, steps.length)}`,
        );
      }
    } catch (error) {
      failure = error;
    }

    if (failure == undefined) {
      storage.set(storageKey, { status: "success" });
      if (step.on_success) {
        await this.processStep(step.on_success, `${pathContext}/on_success`);
      }
    } else if (failure instanceof TaskFailure) {
      storage.set(storageKey, { status: "failure" });
      if (step.on_failure) {
        await this.processStep(step.on_failure, `${pathContext}/on_failure`);
      }
    } else if (failure instanceof TaskErrored) {
      storage.set(storageKey, { status: "error" });
      if (step.on_error) {
        await this.processStep(step.on_error, `${pathContext}/on_error`);
      }
    } else if (failure instanceof TaskAbort) {
      storage.set(storageKey, { status: "abort" });
      if (step.on_abort) {
        await this.processStep(step.on_abort, `${pathContext}/on_abort`);
      }
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

    // Determine version mode: "latest", "every", or "pinned"
    const versionMode = this.getVersionMode(step);

    // Check if this is a native resource by checking the resource type name
    const isNative = nativeResources.isNative(resource?.type);

    // Scope resource name to pipeline to avoid cross-pipeline version sharing
    const scopedResourceName = this.getScopedResourceName(resource?.name!);

    // Get last known version for 'every' mode check using dedicated resource version API
    let lastKnownVersion: ResourceVersion | undefined;
    if (versionMode === "every") {
      try {
        const stored = storage.getLatestResourceVersion(scopedResourceName);
        lastKnownVersion = stored?.version;
      } catch (_e) {
        // No previous version stored, that's fine
      }
    }

    // Determine which version to fetch
    let versionToFetch: ResourceVersion;

    if (versionMode === "pinned") {
      // Pinned version - use exact version specified
      versionToFetch = step.version as ResourceVersion;
    } else {
      // Check for new versions
      let versions: ResourceVersion[];

      if (isNative) {
        const checkResult = nativeResources.check({
          type: resource?.type!,
          source: resource?.source!,
          version: lastKnownVersion,
        });
        versions = checkResult.versions;
      } else {
        // Container-based resource
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
            version: lastKnownVersion,
          }),
          `${pathContext}/check`,
        );

        const checkPayload = JSON.parse(checkResult.stdout);
        versions = checkPayload;
      }

      if (versions.length === 0) {
        throw new Error(`No versions found for resource ${resource?.name}`);
      }

      if (versionMode === "every") {
        // For 'every' mode, get ALL stored versions to filter against
        // This ensures we don't re-process versions we've already seen
        const storedVersions = storage.listResourceVersions(
          scopedResourceName,
          0, // 0 = no limit, get all versions
        );

        // Filter discovered versions against what we've already processed
        const processedVersionSet = new Set(
          storedVersions.map((sv) => JSON.stringify(sv.version)),
        );

        const newVersions = versions.filter(
          (v) => !processedVersionSet.has(JSON.stringify(v)),
        );

        if (newVersions.length === 0) {
          // No new versions, use the latest one we know about
          versionToFetch = versions[versions.length - 1];
        } else {
          // For now, process the first new version (true fan-out requires pipeline-level changes)
          // In a full implementation, this would trigger multiple job runs
          versionToFetch = newVersions[0];
        }
      } else {
        // "latest" mode - get the most recent version (last in array from check)
        versionToFetch = versions[versions.length - 1];
      }
    }

    // Fetch the version
    if (isNative) {
      // Create a volume for the resource output
      const volume = await runtime.createVolume({ name: resource?.name });

      // Register the volume for use by subsequent tasks
      this.taskRunner.getKnownMounts()[resource?.name!] = volume;

      // Use native resource fetch with the volume's absolute path
      nativeResources.fetch({
        type: resource?.type!,
        source: resource?.source!,
        version: versionToFetch,
        params: step.params as { [key: string]: unknown },
        destDir: volume.path,
      });

      // Store in task storage for tracking
      const storageKey = `${this.getBaseStorageKey()}/${pathContext}`;
      storage.set(storageKey, {
        status: "success",
        version: versionToFetch,
        resource: resource?.name,
      });
    } else {
      // Container-based resource
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
            outputs: [{ name: resource?.name! }],
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
          version: versionToFetch,
        }),
        `${pathContext}/get`,
      );
    }

    // Store version using dedicated resource version API for:
    // 1. Global tracking (for 'every' mode across runs)
    // 2. Job input tracking (for 'passed' constraints)
    storage.saveResourceVersion(
      scopedResourceName,
      versionToFetch as { [key: string]: string },
      this.jobConfig.name,
    );
  }

  private getVersionMode(step: Get): "latest" | "every" | "pinned" {
    if (!step.version) {
      return "latest";
    }

    if (typeof step.version === "string") {
      return step.version === "every" ? "every" : "latest";
    }

    return "pinned";
  }

  // Generate a pipeline-scoped resource name to ensure resource versions
  // are isolated per pipeline and not shared globally
  private getScopedResourceName(resourceName: string): string {
    const pipelineID =
      (typeof pipelineContext !== "undefined" && pipelineContext.pipelineID)
        ? pipelineContext.pipelineID
        : "default";
    return `${pipelineID}/${resourceName}`;
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

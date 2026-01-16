// src/task_runner.ts
var TaskRunner = class {
  constructor(taskNames) {
    this.taskNames = taskNames;
  }
  knownMounts = {};
  async runTask(step, stdin, storageKey) {
    const taskStorageKey = storageKey;
    const mounts = await this.prepareMounts(step);
    this.taskNames.push(step.task);
    storage.set(
      taskStorageKey,
      {
        status: "pending"
      }
    );
    let result;
    try {
      result = await runtime.run({
        command: {
          path: step.config.run.path,
          args: step.config.run.args || [],
          user: step.config.run.user
        },
        container_limits: step.config.container_limits,
        env: step.config.env,
        image: step.config?.image_resource.source.repository,
        name: step.task,
        mounts,
        privileged: step.privileged ?? false,
        stdin: stdin ?? "",
        timeout: step.timeout
      });
      let status = "success";
      if (result.status == "abort") {
        status = "abort";
      } else if (result.code !== 0) {
        status = "failure";
      }
      storage.set(
        taskStorageKey,
        {
          status,
          code: result.code,
          stdout: result.stdout,
          stderr: result.stderr
        }
      );
      this.validateTaskResult(step, result);
      return result;
    } catch (error) {
      storage.set(taskStorageKey, { status: "error" });
      throw new TaskErrored(
        `Task ${step.task} errored with message ${error}`
      );
    }
  }
  getKnownMounts() {
    return this.knownMounts;
  }
  async prepareMounts(step) {
    const mounts = {};
    const inputs = step.config.inputs || [];
    const outputs = step.config.outputs || [];
    const caches = step.config.caches || [];
    for (const mount of inputs) {
      this.knownMounts[mount.name] ||= await runtime.createVolume();
      mounts[mount.name] = this.knownMounts[mount.name];
    }
    for (const mount of outputs) {
      this.knownMounts[mount.name] ||= await runtime.createVolume();
      mounts[mount.name] = this.knownMounts[mount.name];
    }
    for (const cache of caches) {
      const cacheName = this.pathToCacheName(cache.path);
      this.knownMounts[cacheName] ||= await runtime.createVolume({
        name: cacheName
      });
      const mountPath = cache.path.replace(/^\/+/, "");
      mounts[mountPath] = this.knownMounts[cacheName];
    }
    return mounts;
  }
  // Convert a cache path to a safe volume name
  pathToCacheName(path) {
    return "cache-" + path.replace(/^\/+/, "").replace(/[^a-zA-Z0-9]+/g, "-").replace(/-+/g, "-").replace(/-$/, "").toLowerCase();
  }
  validateTaskResult(step, result) {
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
};
var CustomError = class extends Error {
  constructor(message) {
    super(message);
    this.name = this.constructor.name;
  }
};
var TaskFailure = class extends CustomError {
};
var TaskErrored = class extends CustomError {
};
var TaskAbort = class extends CustomError {
};

// src/job_runner.ts
function zeroPad(num, places) {
  return String(num).padStart(places, "0");
}
function zeroPadWithLength(num, length) {
  const decimalPlaces = String(length).split(".")[1]?.length || 0;
  return zeroPad(num, decimalPlaces);
}
var buildID = typeof pipelineContext !== "undefined" && pipelineContext.runID ? pipelineContext.runID : zeroPad(Date.now(), 20);
var JobRunner = class {
  constructor(jobConfig, resources, resourceTypes) {
    this.jobConfig = jobConfig;
    this.resources = resources;
    this.resourceTypes = resourceTypes;
    this.buildID = buildID;
    this.taskRunner = new TaskRunner(this.taskNames);
  }
  taskNames = [];
  taskRunner;
  buildID;
  async run() {
    const storageKey = this.getBaseStorageKey();
    let failure = void 0;
    const dependsOn = this.extractDependencies();
    storage.set(storageKey, { status: "pending", dependsOn });
    try {
      for (let i = 0; i < this.jobConfig.plan.length; i++) {
        await this.processStep(
          this.jobConfig.plan[i],
          zeroPadWithLength(i, this.jobConfig.plan.length)
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
      if (failure === void 0 && this.jobConfig.on_success) {
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
  extractDependencies() {
    const dependencies = [];
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
  getBaseStorageKey() {
    return `/pipeline/${this.buildID}/jobs/${this.jobConfig.name}`;
  }
  getStepIdentifier(step) {
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
      const name = Array.isArray(step.notify) ? step.notify.join("-") : step.notify;
      return `notify/${name}`;
    }
    return "unknown";
  }
  generateStorageKeyForStep(step, currentPath) {
    const basePath = this.getBaseStorageKey();
    const stepId = this.getStepIdentifier(step);
    return `${basePath}/${currentPath}/${stepId}`;
  }
  async processStep(step, pathContext) {
    const maxAttempts = step.attempts || 1;
    for (let attempt = 1; attempt <= maxAttempts; attempt++) {
      try {
        await this.processStepInternal(step, pathContext);
        return;
      } catch (error) {
        if (attempt < maxAttempts) {
          console.log(`Attempt ${attempt}/${maxAttempts} failed, retrying...`);
          continue;
        }
        throw error;
      }
    }
  }
  async processStepInternal(step, pathContext) {
    if (step.across && step.across.length > 0) {
      await this.processAcrossStep(step, pathContext);
      return;
    }
    if ("get" in step) {
      await this.processGetStep(
        step,
        `${pathContext}/${this.getStepIdentifier(step)}`
      );
    } else if ("do" in step) {
      await this.processDoStep(
        step,
        `${pathContext}/${this.getStepIdentifier(step)}`
      );
    } else if ("put" in step) {
      await this.processPutStep(
        step,
        `${pathContext}/${this.getStepIdentifier(step)}`
      );
    } else if ("try" in step) {
      await this.processTryStep(
        step,
        `${pathContext}/${this.getStepIdentifier(step)}`
      );
    } else if ("task" in step) {
      await this.processTaskStep(
        step,
        `${pathContext}/${this.getStepIdentifier(step)}`
      );
    } else if ("in_parallel" in step) {
      await this.processParallelSteps(
        step,
        `${pathContext}/${this.getStepIdentifier(step)}`
      );
    } else if ("notify" in step) {
      await this.processNotifyStep(
        step,
        `${pathContext}/${this.getStepIdentifier(step)}`
      );
    }
  }
  async getFile(file, pathContext) {
    const mountName = file.split("/")[0];
    const result = await this.runTask(
      {
        task: `get-file-${file}`,
        config: {
          image_resource: {
            type: "registry-image",
            source: {
              repository: "busybox"
            }
          },
          inputs: [
            { name: mountName }
          ],
          run: {
            path: "sh",
            args: ["-c", `cat ${file}`]
          }
        },
        assert: {
          code: 0
        }
      },
      void 0,
      pathContext
    );
    return result.stdout;
  }
  async processTaskStep(step, pathContext) {
    if ("file" in step) {
      const contents = await this.getFile(step.file, pathContext);
      const taskConfig = YAML.parse(contents);
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
          timeout: step.timeout
        },
        void 0,
        pathContext
      );
    } else {
      await this.runTask(step, void 0, pathContext);
    }
  }
  async processParallelSteps(step, pathContext) {
    await this.processDoStep(step, pathContext);
  }
  async processAcrossStep(step, pathContext) {
    const combinations = this.generateAcrossCombinations(step.across);
    const storageKey = `${this.getBaseStorageKey()}/${pathContext}/across`;
    storage.set(storageKey, { status: "pending", total: combinations.length });
    let failureOccurred = false;
    const failFast = step.fail_fast || false;
    for (let i = 0; i < combinations.length; i++) {
      if (failureOccurred && failFast) {
        break;
      }
      const combination = combinations[i];
      const varContext = Object.entries(combination).map(([key, value]) => `${key}_${value}`).join("_");
      const modifiedStep = this.injectAcrossVariables(step, combination);
      try {
        await this.processStepInternal(
          modifiedStep,
          `${pathContext}/across/${i}_${varContext}`
        );
      } catch (error) {
        failureOccurred = true;
        if (failFast) {
          storage.set(storageKey, { status: "failure", failed_at: i });
          throw error;
        }
        console.error(`Across combination ${i} failed:`, error);
      }
    }
    if (failureOccurred && !failFast) {
      storage.set(storageKey, { status: "failure" });
      throw new TaskFailure("One or more across combinations failed");
    }
    storage.set(storageKey, { status: "success", total: combinations.length });
  }
  generateAcrossCombinations(acrossVars) {
    if (acrossVars.length === 0) {
      return [{}];
    }
    const [first, ...rest] = acrossVars;
    const restCombinations = this.generateAcrossCombinations(rest);
    const combinations = [];
    for (const value of first.values) {
      for (const restCombination of restCombinations) {
        combinations.push({
          [first.var]: value,
          ...restCombination
        });
      }
    }
    return combinations;
  }
  injectAcrossVariables(step, variables) {
    const clonedStep = { ...step };
    if ("task" in clonedStep && clonedStep.config) {
      clonedStep.config = {
        ...clonedStep.config,
        env: {
          ...clonedStep.config.env,
          ...variables
        }
      };
    }
    delete clonedStep.across;
    delete clonedStep.fail_fast;
    return clonedStep;
  }
  async processTryStep(step, pathContext) {
    try {
      await this.processDoStep(step, pathContext);
    } catch (_err) {
    } finally {
      storage.set(pathContext, { status: "success" });
    }
  }
  async processNotifyStep(step, pathContext) {
    const storageKey = `${this.getBaseStorageKey()}/${pathContext}`;
    let failure = void 0;
    try {
      storage.set(storageKey, { status: "pending" });
      notify.updateJobName(this.jobConfig.name);
      notify.updateStatus("running");
      const names = Array.isArray(step.notify) ? step.notify : [step.notify];
      if (step.async) {
        for (const name of names) {
          notify.send({ name, message: step.message, async: true });
        }
        storage.set(storageKey, { status: "success" });
      } else {
        if (names.length === 1) {
          await notify.send({
            name: names[0],
            message: step.message,
            async: false
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
    if (failure === void 0 && step.on_success) {
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
  async processDoStep(step, pathContext) {
    const storageKey = `${this.getBaseStorageKey()}/${pathContext}`;
    let failure = void 0;
    try {
      storage.set(storageKey, { status: "pending" });
      let steps = [];
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
          `${pathContext}/${zeroPadWithLength(i, steps.length)}`
        );
      }
    } catch (error) {
      failure = error;
    }
    if (failure == void 0) {
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
  async processPutStep(step, pathContext) {
    const resource = this.findResource(step.put);
    const resourceType = this.findResourceType(resource?.type);
    const putResponse = await this.runTask(
      {
        task: `put-${resource?.name}`,
        config: {
          image_resource: {
            type: "registry-image",
            source: {
              repository: resourceType?.source.repository
            }
          },
          outputs: [
            { name: resource?.name }
          ],
          run: {
            path: "/opt/resource/out",
            args: [`./${resource?.name}`]
          }
        },
        assert: {
          code: 0
        },
        ensure: step.ensure,
        on_success: step.on_success,
        on_failure: step.on_failure,
        on_error: step.on_error,
        on_abort: step.on_abort,
        timeout: step.timeout
      },
      JSON.stringify({
        source: resource?.source,
        params: step.params
      }),
      `${pathContext}/put`
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
              repository: resourceType?.source.repository
            }
          },
          outputs: [
            { name: resource?.name }
          ],
          run: {
            path: "/opt/resource/in",
            args: [`./${resource?.name}`]
          }
        },
        assert: {
          code: 0
        },
        ensure: step.ensure,
        on_success: step.on_success,
        on_failure: step.on_failure,
        on_error: step.on_error,
        on_abort: step.on_abort,
        timeout: step.timeout
      },
      JSON.stringify({
        source: resource?.source,
        version
      }),
      `${pathContext}/get`
    );
  }
  async processGetStep(step, pathContext) {
    const resource = this.findResource(step.get);
    const resourceType = this.findResourceType(resource?.type);
    const versionMode = this.getVersionMode(step);
    const isNative = nativeResources.isNative(resource?.type);
    const scopedResourceName = this.getScopedResourceName(resource?.name);
    let lastKnownVersion;
    if (versionMode === "every") {
      try {
        const stored = storage.getLatestResourceVersion(scopedResourceName);
        lastKnownVersion = stored?.version;
      } catch (_e) {
      }
    }
    let versionToFetch;
    if (versionMode === "pinned") {
      versionToFetch = step.version;
    } else {
      let versions;
      if (isNative) {
        const checkResult = nativeResources.check({
          type: resource?.type,
          source: resource?.source,
          version: lastKnownVersion
        });
        versions = checkResult.versions;
      } else {
        const checkResult = await this.runTask(
          {
            task: `check-${resource?.name}`,
            config: {
              image_resource: {
                type: "registry-image",
                source: {
                  repository: resourceType?.source.repository
                }
              },
              run: {
                path: "/opt/resource/check"
              }
            },
            assert: {
              code: 0
            },
            ensure: step.ensure,
            on_success: step.on_success,
            on_failure: step.on_failure,
            on_error: step.on_error,
            on_abort: step.on_abort,
            timeout: step.timeout
          },
          JSON.stringify({
            source: resource?.source,
            version: lastKnownVersion
          }),
          `${pathContext}/check`
        );
        const checkPayload = JSON.parse(checkResult.stdout);
        versions = checkPayload;
      }
      if (versions.length === 0) {
        throw new Error(`No versions found for resource ${resource?.name}`);
      }
      if (versionMode === "every") {
        const storedVersions = storage.listResourceVersions(
          scopedResourceName,
          0
          // 0 = no limit, get all versions
        );
        const processedVersionSet = new Set(
          storedVersions.map((sv) => JSON.stringify(sv.version))
        );
        const newVersions = versions.filter(
          (v) => !processedVersionSet.has(JSON.stringify(v))
        );
        if (newVersions.length === 0) {
          versionToFetch = versions[versions.length - 1];
        } else {
          versionToFetch = newVersions[0];
        }
      } else {
        versionToFetch = versions[versions.length - 1];
      }
    }
    if (isNative) {
      const volume = await runtime.createVolume({ name: resource?.name });
      this.taskRunner.getKnownMounts()[resource?.name] = volume;
      nativeResources.fetch({
        type: resource?.type,
        source: resource?.source,
        version: versionToFetch,
        params: step.params,
        destDir: volume.path
      });
      const storageKey = `${this.getBaseStorageKey()}/${pathContext}`;
      storage.set(storageKey, {
        status: "success",
        version: versionToFetch,
        resource: resource?.name
      });
    } else {
      await this.runTask(
        {
          task: `get-${resource?.name}`,
          config: {
            image_resource: {
              type: "registry-image",
              source: {
                repository: resourceType?.source.repository
              }
            },
            outputs: [{ name: resource?.name }],
            run: {
              path: "/opt/resource/in",
              args: [`./${resource?.name}`]
            }
          },
          assert: {
            code: 0
          },
          ensure: step.ensure,
          on_success: step.on_success,
          on_failure: step.on_failure,
          on_error: step.on_error,
          on_abort: step.on_abort,
          timeout: step.timeout
        },
        JSON.stringify({
          source: resource?.source,
          version: versionToFetch
        }),
        `${pathContext}/get`
      );
    }
    storage.saveResourceVersion(
      scopedResourceName,
      versionToFetch,
      this.jobConfig.name
    );
  }
  getVersionMode(step) {
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
  getScopedResourceName(resourceName) {
    const pipelineID = typeof pipelineContext !== "undefined" && pipelineContext.pipelineID ? pipelineContext.pipelineID : "default";
    return `${pipelineID}/${resourceName}`;
  }
  findResource(resourceName) {
    const resource = this.resources.find(
      (resource2) => resource2.name === resourceName
    );
    return resource;
  }
  findResourceType(typeName) {
    const resourceType = this.resourceTypes.find(
      (type) => type.name === typeName
    );
    return resourceType;
  }
  async runTask(step, stdin, pathContext = "") {
    const storageKey = `${this.getBaseStorageKey()}/${pathContext}`;
    let result;
    try {
      result = await this.taskRunner.runTask(step, stdin, storageKey);
    } catch (error) {
      if (step.on_error) {
        await this.processStep(step.on_error, `${pathContext}/on_error`);
      }
      throw new TaskErrored(
        `Task ${step.task} errored with message ${error}`
      );
    }
    if (result.code === 0 && result.status == "complete" && step.on_success) {
      await this.processStep(step.on_success, `${pathContext}/on_success`);
    } else if (result.code !== 0 && result.status == "complete" && step.on_failure) {
      await this.processStep(step.on_failure, `${pathContext}/on_failure`);
    } else if (result.status == "abort" && step.on_abort) {
      await this.processStep(step.on_abort, `${pathContext}/on_abort`);
    }
    if (step.ensure) {
      await this.processStep(step.ensure, `${pathContext}/ensure`);
    }
    if (result.code > 0) {
      throw new TaskFailure(
        `Task ${step.task} failed with code ${result.code}`
      );
    } else if (result.status == "abort") {
      throw new TaskAbort(
        `Task ${step.task} aborted with message ${result.message}`
      );
    }
    return result;
  }
};

// src/pipeline_runner.ts
var PipelineRunner = class {
  constructor(config) {
    this.config = config;
    this.validatePipelineConfig();
    this.initializeNotifications();
  }
  jobResults = /* @__PURE__ */ new Map();
  executedJobs = [];
  initializeNotifications() {
    if (this.config.notifications) {
      notify.setConfigs(this.config.notifications);
    }
    const buildID2 = typeof pipelineContext !== "undefined" && pipelineContext.runID ? pipelineContext.runID : String(Date.now());
    notify.setContext({
      pipelineName: this.config.jobs[0]?.name || "unknown",
      jobName: "",
      buildID: buildID2,
      status: "pending",
      startTime: (/* @__PURE__ */ new Date()).toISOString(),
      endTime: "",
      duration: "",
      environment: {},
      taskResults: {}
    });
  }
  validatePipelineConfig() {
    assert.truthy(
      this.config.jobs.length > 0,
      "Pipeline must have at least one job"
    );
    assert.truthy(
      this.config.jobs.every((job) => job.plan.length > 0),
      "Every job must have at least one step"
    );
    const jobNames = this.config.jobs.map((job) => job.name);
    assert.equal(
      jobNames.length,
      new Set(jobNames).size,
      "Job names must be unique"
    );
    if (this.config.jobs.length > 1) {
      this.validateJobDependencies();
    }
    if (this.config.resources.length > 0) {
      this.validateResources();
    }
  }
  validateJobDependencies() {
    const jobNames = new Set(this.config.jobs.map((job) => job.name));
    assert.truthy(
      this.config.jobs.every(
        (job) => job.plan.every((step) => {
          if ("get" in step && step.passed) {
            return step.passed.every((passedJob) => jobNames.has(passedJob));
          }
          return true;
        })
      ),
      "All passed constraints must reference existing jobs"
    );
    this.detectCircularDependencies();
  }
  detectCircularDependencies() {
    const graph = {};
    for (const job of this.config.jobs) {
      graph[job.name] = [];
    }
    for (const job of this.config.jobs) {
      for (const step of job.plan) {
        if ("get" in step && step.passed) {
          for (const dependency of step.passed) {
            graph[dependency].push(job.name);
          }
        }
      }
    }
    const visited = /* @__PURE__ */ new Set();
    const recStack = /* @__PURE__ */ new Set();
    const hasCycle = (node) => {
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
  validateResources() {
    assert.truthy(
      this.config.resources.every(
        (resource) => this.config.resource_types.some((type) => type.name === resource.type)
      ),
      "Every resource must have a valid resource type"
    );
    assert.truthy(
      this.config.jobs.every(
        (job) => job.plan.every((step) => {
          if ("get" in step) {
            return this.config.resources.some(
              (resource) => resource.name === step.get
            );
          }
          return true;
        })
      ),
      "Every get must have a resource reference"
    );
  }
  async run() {
    const jobsWithNoDeps = this.findJobsWithNoDependencies();
    for (const job of jobsWithNoDeps) {
      await this.runJob(job);
    }
    if (this.config.assert?.execution) {
      assert.equal(this.executedJobs, this.config.assert.execution);
    }
  }
  findJobsWithNoDependencies() {
    return this.config.jobs.filter((job) => {
      return !job.plan.some((step) => {
        if ("get" in step && step.passed) {
          return true;
        }
        return false;
      });
    });
  }
  async runJob(job) {
    this.executedJobs.push(job.name);
    try {
      const jobRunner = new JobRunner(
        job,
        this.config.resources,
        this.config.resource_types
      );
      await jobRunner.run();
      this.jobResults.set(job.name, true);
      await this.runDependentJobs(job.name);
    } catch (error) {
      this.jobResults.set(job.name, false);
      throw error;
    }
  }
  async runDependentJobs(completedJobName) {
    const dependentJobs = this.findDependentJobs(completedJobName);
    for (const job of dependentJobs) {
      const canRun = this.canJobRun(job);
      if (canRun) {
        await this.runJob(job);
      }
    }
  }
  findDependentJobs(jobName) {
    return this.config.jobs.filter((job) => {
      return job.plan.some((step) => {
        if ("get" in step && step.passed && step.passed.includes(jobName)) {
          return true;
        }
        return false;
      });
    });
  }
  canJobRun(job) {
    for (const step of job.plan) {
      if ("get" in step && step.passed && step.passed.length > 0) {
        const allDependenciesMet = step.passed.every(
          (depJobName) => this.jobResults.get(depJobName) === true
        );
        if (!allDependenciesMet) {
          return false;
        }
      }
    }
    return true;
  }
};

// src/index.ts
function createPipeline(config) {
  const runner = new PipelineRunner(config);
  return () => runner.run();
}
export {
  createPipeline
};

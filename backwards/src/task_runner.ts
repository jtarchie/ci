/// <reference path="../../packages/ci/src/global.d.ts" />

export class TaskRunner {
  private knownMounts: KnownMounts = {};

  constructor(
    private taskNames: string[],
  ) {}

  async runTask(
    step: Task,
    stdin: string | undefined,
    storageKey: string,
  ): Promise<RunTaskResult> {
    // Use provided storage key or default to a simple task name key
    const taskStorageKey = storageKey;
    const mounts = await this.prepareMounts(step);
    this.taskNames.push(step.task);

    storage.set(
      taskStorageKey,
      {
        status: "pending",
      },
    );

    let result: RunTaskResult;

    try {
      result = await runtime.run({
        command: {
          path: step.config.run.path,
          args: step.config.run.args || [],
          user: step.config.run.user,
        },
        container_limits: step.container_limits ?? step.config.container_limits,
        env: step.config.env,
        image: step.config?.image_resource.source.repository!,
        name: step.task,
        mounts: mounts,
        privileged: step.privileged ?? false,
        stdin: stdin ?? "",
        timeout: step.timeout,
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
          status: status,
          code: result.code,
          stdout: result.stdout,
          stderr: result.stderr,
        },
      );

      this.validateTaskResult(step, result);

      return result;
    } catch (error) {
      storage.set(taskStorageKey, { status: "error" });

      throw new TaskErrored(
        `Task ${step.task} errored with message ${error}`,
      );
    }
  }

  getKnownMounts(): KnownMounts {
    return this.knownMounts;
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

export class TaskFailure extends CustomError {}
export class TaskErrored extends CustomError {}
export class TaskAbort extends CustomError {}

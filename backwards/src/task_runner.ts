export class TaskRunner {
  private knownMounts: KnownMounts = {};

  constructor(
    private job: Job,
    private storagePathPrefix: string,
    private taskNames: string[],
  ) {}

  async runTask(step: Task, stdin?: string): Promise<RunTaskResult> {
    const storageKey = `${this.storagePathPrefix}/tasks/${step.task}`;
    const mounts = await this.prepareMounts(step);
    this.taskNames.push(step.task);

    storage.set(
      storageKey,
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
        env: step.config.env,
        image: step.config?.image_resource.source.repository!,
        name: step.task,
        mounts: mounts,
        privileged: step.privileged ?? false,
        stdin: stdin ?? "",
        timeout: step.timeout,
      });

      storage.set(
        storageKey,
        {
          status: result.code === 0 ? "success" : "failure",
          code: result.code,
          stdout: result.stdout,
          stderr: result.stderr,
        },
      );

      this.validateTaskResult(step, result);

      return result;
    } catch (error) {
      throw new TaskErrored(
        `Task ${step.task} errored with message ${error}`,
      );
    }
  }

  async getFile(file: string, mountName: string): Promise<string> {
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

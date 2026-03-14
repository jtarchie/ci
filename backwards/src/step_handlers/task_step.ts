/// <reference path="../../../packages/pocketci/src/global.d.ts" />

import { TaskFailure } from "../task_runner.ts";
import type { StepContext } from "./step_context.ts";
import type { StepHandler } from "./step_handler.ts";

export class TaskStepHandler implements StepHandler {
  getIdentifier(step: Step): string {
    return `tasks/${(step as Task).task}`;
  }

  async process(
    ctx: StepContext,
    step: Task,
    pathContext: string,
  ): Promise<void> {
    let taskStep = step;

    if ("file" in step) {
      const contents = await this.getFile(ctx, step.file!, pathContext);
      const taskConfig = YAML.parse(contents) as TaskConfig;
      taskStep = {
        task: step.task,
        parallelism: step.parallelism,
        config: taskConfig,
        assert: step.assert,
        ensure: step.ensure,
        on_success: step.on_success,
        on_failure: step.on_failure,
        on_error: step.on_error,
        on_abort: step.on_abort,
        timeout: step.timeout,
      };
    }

    const parallelism = taskStep.parallelism || 1;
    if (parallelism <= 1) {
      await ctx.runTask(taskStep, undefined, pathContext);
      return;
    }

    const storageKey =
      `${ctx.paths.getBaseStorageKey()}/${pathContext}/parallelism`;
    storage.set(storageKey, { status: "pending", total: parallelism });

    const indexes = Array.from({ length: parallelism }, (_, i) => i + 1);
    const result = await ctx.concurrency.runWithConcurrencyLimit(
      indexes,
      async (parallelIndex) => {
        const indexedTask: Task = {
          ...taskStep,
          task: `${taskStep.task}-${parallelIndex}`,
          config: {
            ...taskStep.config,
            env: {
              ...taskStep.config.env,
              CI_TASK_COUNT: String(parallelism),
              CI_TASK_INDEX: String(parallelIndex),
            },
          },
        };

        await ctx.runTask(
          indexedTask,
          undefined,
          `${pathContext}/parallelism/${parallelIndex}`,
        );
      },
    );

    if (result.failed) {
      storage.set(storageKey, { status: "failure", total: parallelism });
      throw result.firstError ??
        new TaskFailure("One or more parallel task instances failed");
    }

    storage.set(storageKey, { status: "success", total: parallelism });
  }

  private async getFile(
    ctx: StepContext,
    file: string,
    pathContext: string,
  ): Promise<string> {
    const mountName = file.split("/")[0];
    const result = await ctx.runTask(
      {
        task: `get-file-${file}`,
        config: {
          image_resource: {
            type: "registry-image",
            source: { repository: "busybox" },
          },
          inputs: [{ name: mountName }],
          run: { path: "sh", args: ["-c", `cat ${file}`] },
        },
        assert: { code: 0 },
      },
      undefined,
      pathContext,
    );
    return result.stdout;
  }
}

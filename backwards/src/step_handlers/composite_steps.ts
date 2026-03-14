/// <reference path="../../../packages/pocketci/src/global.d.ts" />

import { TaskAbort, TaskErrored, TaskFailure } from "../task_runner.ts";
import { zeroPadWithLength } from "../job_storage_paths.ts";
import type { StepContext } from "./step_context.ts";

export async function processDoStep(
  ctx: StepContext,
  step: Do | Try | InParallel,
  pathContext: string,
): Promise<void> {
  const storageKey = `${ctx.paths.getBaseStorageKey()}/${pathContext}`;
  let failure: unknown = undefined;
  const isTryStep = "try" in step;

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

    if ("in_parallel" in step) {
      const result = await ctx.concurrency.runWithConcurrencyLimit(
        steps,
        async (subStep, i) => {
          await ctx.processStep(
            subStep,
            `${pathContext}/${zeroPadWithLength(i, steps.length)}`,
          );
        },
        step.in_parallel.limit,
        step.in_parallel.fail_fast,
      );

      if (result.failed) {
        throw result.firstError;
      }
    } else {
      for (let i = 0; i < steps.length; i++) {
        await ctx.processStep(
          steps[i],
          `${pathContext}/${zeroPadWithLength(i, steps.length)}`,
        );
      }
    }
  } catch (error) {
    failure = error;
  }

  if (failure == undefined) {
    storage.set(storageKey, { status: "success" });
    if (step.on_success) {
      await ctx.processStep(step.on_success, `${pathContext}/on_success`);
    }
  } else if (failure instanceof TaskFailure) {
    storage.set(storageKey, { status: "failure" });
    if (step.on_failure) {
      await ctx.processStep(step.on_failure, `${pathContext}/on_failure`);
    }
  } else if (failure instanceof TaskErrored) {
    storage.set(storageKey, { status: "error" });
    if (step.on_error) {
      await ctx.processStep(step.on_error, `${pathContext}/on_error`);
    }
  } else if (failure instanceof TaskAbort) {
    storage.set(storageKey, { status: "abort" });
    if (step.on_abort) {
      await ctx.processStep(step.on_abort, `${pathContext}/on_abort`);
    }
  }

  if (step.ensure) {
    await ctx.processStep(step.ensure, `${pathContext}/ensure`);
  }

  if (failure && !isTryStep) {
    throw failure;
  }
}

export async function processTryStep(
  ctx: StepContext,
  step: Try,
  pathContext: string,
): Promise<void> {
  try {
    await processDoStep(ctx, step, pathContext);
  } catch (_err) {
    // do nothing
  } finally {
    storage.set(pathContext, { status: "success" });
  }
}

export async function processParallelSteps(
  ctx: StepContext,
  step: InParallel,
  pathContext: string,
): Promise<void> {
  await processDoStep(ctx, step, pathContext);
}

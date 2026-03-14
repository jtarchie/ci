/// <reference path="../../../packages/pocketci/src/global.d.ts" />

import { zeroPadWithLength } from "../job_storage_paths.ts";
import type { StepContext } from "./step_context.ts";
import type { StepHandler } from "./step_handler.ts";
import { processHooks } from "./resource_helpers.ts";

export class DoStepHandler implements StepHandler {
  getIdentifier(_step: Step): string {
    return "do";
  }

  async process(
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

    await processHooks(ctx, step, pathContext, storageKey, failure);

    if (failure && !isTryStep) {
      throw failure;
    }
  }
}

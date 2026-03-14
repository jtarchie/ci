/// <reference path="../../../packages/pocketci/src/global.d.ts" />

import { TaskFailure } from "../task_runner.ts";
import type { StepContext } from "./step_context.ts";

export async function processAcrossStep(
  ctx: StepContext,
  step: Step,
  pathContext: string,
  processStepInternal: (
    step: Step,
    pathContext: string,
    attempt?: number,
  ) => Promise<void>,
): Promise<void> {
  const combinations = ctx.variableResolver.generateAcrossCombinations(
    step.across!,
  );

  const storageKey = `${ctx.paths.getBaseStorageKey()}/${pathContext}/across`;
  storage.set(storageKey, { status: "pending", total: combinations.length });

  let failureOccurred = false;
  const failFast = (step as { fail_fast?: boolean }).fail_fast || false;

  const acrossLimitCandidates = step.across!
    .map((acrossVar) => acrossVar.max_in_flight)
    .filter((limit): limit is number => Boolean(limit && limit > 0));
  const acrossLimit = acrossLimitCandidates.length > 0
    ? Math.min(...acrossLimitCandidates)
    : 1;
  const effectiveAcrossLimit = failFast ? 1 : acrossLimit;

  const result = await ctx.concurrency.runWithConcurrencyLimit(
    combinations,
    async (combination, i) => {
      const varContext = Object.entries(combination)
        .map(([key, value]) => `${key}_${value}`)
        .join("_");

      const modifiedStep = ctx.variableResolver.injectAcrossVariables(
        step,
        combination,
      );

      try {
        await processStepInternal(
          modifiedStep,
          `${pathContext}/across/${i}_${varContext}`,
        );
      } catch (error) {
        failureOccurred = true;
        console.error(`Across combination ${i} failed:`, error);
        throw error;
      }
    },
    effectiveAcrossLimit,
    failFast,
  );

  if (result.failed) {
    failureOccurred = true;
    if (failFast) {
      storage.set(storageKey, { status: "failure" });
      throw result.firstError ??
        new TaskFailure("One or more across combinations failed");
    }
  }

  if (failureOccurred) {
    storage.set(storageKey, { status: "failure" });
    throw new TaskFailure("One or more across combinations failed");
  }

  storage.set(storageKey, { status: "success", total: combinations.length });
}

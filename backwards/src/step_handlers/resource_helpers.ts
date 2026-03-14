/// <reference path="../../../packages/pocketci/src/global.d.ts" />

import { failureHook, failureStatus } from "../utils.ts";
import type { StepContext } from "./step_context.ts";

export function findResource(resources: Resource[], name: string): Resource {
  return resources.find((r) => r.name === name)!;
}

export function findResourceType(
  resourceTypes: ResourceType[],
  typeName?: string,
): ResourceType {
  return resourceTypes.find((t) => t.name === typeName)!;
}

export function stepHooks(step: Get | Put) {
  return {
    ensure: step.ensure,
    on_success: step.on_success,
    on_failure: step.on_failure,
    on_error: step.on_error,
    on_abort: step.on_abort,
    timeout: step.timeout,
  };
}

export async function processHooks(
  ctx: StepContext,
  step: Step,
  pathContext: string,
  storageKey: string,
  failure: unknown,
): Promise<void> {
  storage.set(storageKey, { status: failureStatus(failure) });

  const hookName = failureHook(failure);
  if (hookName && (step as Record<string, unknown>)[hookName]) {
    await ctx.processStep(
      (step as Record<string, unknown>)[hookName] as Step,
      `${pathContext}/${hookName}`,
    );
  }

  if (step.ensure) {
    await ctx.processStep(step.ensure, `${pathContext}/ensure`);
  }
}

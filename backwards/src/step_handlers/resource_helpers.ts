/// <reference path="../../../packages/pocketci/src/global.d.ts" />

import { TaskAbort, TaskErrored, TaskFailure } from "../task_runner.ts";
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
}

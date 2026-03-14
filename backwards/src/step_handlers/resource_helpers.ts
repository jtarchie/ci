/// <reference path="../../../packages/pocketci/src/global.d.ts" />

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

/// <reference path="../../../packages/pocketci/src/global.d.ts" />

import {
  getLatestResourceVersion,
  listResourceVersions,
  saveResourceVersion,
} from "../resource_store.ts";
import type { StepContext } from "./step_context.ts";

function getVersionMode(step: Get): "latest" | "every" | "pinned" {
  if (!step.version) return "latest";
  if (typeof step.version === "string") {
    return step.version === "every" ? "every" : "latest";
  }
  return "pinned";
}

function getScopedResourceName(resourceName: string): string {
  const pipelineID =
    (typeof pipelineContext !== "undefined" && pipelineContext.pipelineID)
      ? pipelineContext.pipelineID
      : "default";
  return `${pipelineID}/${resourceName}`;
}

function findResource(resources: Resource[], name: string): Resource {
  return resources.find((r) => r.name === name)!;
}

function findResourceType(
  resourceTypes: ResourceType[],
  typeName?: string,
): ResourceType {
  return resourceTypes.find((t) => t.name === typeName)!;
}

function stepHooks(step: Get | Put) {
  return {
    ensure: step.ensure,
    on_success: step.on_success,
    on_failure: step.on_failure,
    on_error: step.on_error,
    on_abort: step.on_abort,
    timeout: step.timeout,
  };
}

async function resolveVersionToFetch(
  step: Get,
  resource: Resource,
  resourceType: ResourceType,
  versionMode: "latest" | "every" | "pinned",
  scopedResourceName: string,
  isNative: boolean,
  ctx: StepContext,
  pathContext: string,
): Promise<ResourceVersion> {
  if (versionMode === "pinned") {
    return step.version as ResourceVersion;
  }

  let lastKnownVersion: ResourceVersion | undefined;
  if (versionMode === "every") {
    const stored = getLatestResourceVersion(scopedResourceName);
    lastKnownVersion = stored?.version;
  }

  let versions: ResourceVersion[];

  if (isNative) {
    const checkResult = nativeResources.check({
      type: resource.type!,
      source: resource.source!,
      version: lastKnownVersion,
    });
    versions = checkResult.versions;
  } else {
    const checkResult = await ctx.runTask(
      {
        task: `check-${resource.name}`,
        config: {
          image_resource: {
            type: "registry-image",
            source: { repository: resourceType.source.repository! },
          },
          run: { path: "/opt/resource/check" },
        },
        assert: { code: 0 },
        ...stepHooks(step),
      },
      JSON.stringify({ source: resource.source, version: lastKnownVersion }),
      `${pathContext}/check`,
    );
    versions = JSON.parse(checkResult.stdout);
  }

  if (versions.length === 0) {
    throw new Error(`No versions found for resource ${resource.name}`);
  }

  if (versionMode === "every") {
    const storedVersions = listResourceVersions(scopedResourceName, 0);
    const processedSet = new Set(
      storedVersions.map((sv) => JSON.stringify(sv.version)),
    );
    const newVersions = versions.filter(
      (v) => !processedSet.has(JSON.stringify(v)),
    );
    return newVersions.length > 0
      ? newVersions[0]
      : versions[versions.length - 1];
  }

  return versions[versions.length - 1];
}

export async function processGetStep(
  ctx: StepContext,
  step: Get,
  pathContext: string,
): Promise<void> {
  const resource = findResource(ctx.resources, step.get);
  const resourceType = findResourceType(ctx.resourceTypes, resource?.type);
  const versionMode = getVersionMode(step);

  const isNativeDriver = typeof pipelineContext !== "undefined" &&
    pipelineContext.driverName === "native";
  const isNative = isNativeDriver && nativeResources.isNative(resource?.type);
  const scopedResourceName = getScopedResourceName(resource.name!);

  const versionToFetch = await resolveVersionToFetch(
    step,
    resource,
    resourceType,
    versionMode,
    scopedResourceName,
    isNative,
    ctx,
    pathContext,
  );

  if (isNative) {
    const volume = await runtime.createVolume({ name: resource.name });
    ctx.taskRunner.getKnownMounts()[resource.name!] = volume;

    const storageKey = `${ctx.paths.getBaseStorageKey()}/${pathContext}`;
    storage.set(storageKey, { status: "pending", resource: resource.name });

    try {
      nativeResources.fetch({
        type: resource.type!,
        source: resource.source!,
        version: versionToFetch,
        params: step.params as { [key: string]: unknown },
        destDir: volume.path,
      });
      storage.set(storageKey, {
        status: "success",
        version: versionToFetch,
        resource: resource.name,
      });
    } catch (error) {
      storage.set(storageKey, {
        status: "error",
        resource: resource.name,
        error: String(error),
      });
      throw new Error(
        `Failed to fetch resource '${resource.name}': ${error}`,
      );
    }
  } else {
    await ctx.runTask(
      {
        task: `get-${resource.name}`,
        config: {
          image_resource: {
            type: "registry-image",
            source: { repository: resourceType.source.repository! },
          },
          outputs: [{ name: resource.name! }],
          run: { path: "/opt/resource/in", args: [`./${resource.name}`] },
        },
        assert: { code: 0 },
        ...stepHooks(step),
      },
      JSON.stringify({ source: resource.source, version: versionToFetch }),
      `${pathContext}/get`,
    );
  }

  saveResourceVersion(
    scopedResourceName,
    versionToFetch as { [key: string]: string },
    ctx.jobName,
  );
}

export async function processPutStep(
  ctx: StepContext,
  step: Put,
  pathContext: string,
): Promise<void> {
  const resource = findResource(ctx.resources, step.put);
  const resourceType = findResourceType(ctx.resourceTypes, resource?.type);
  const hooks = stepHooks(step);

  const putResponse = await ctx.runTask(
    {
      task: `put-${resource.name}`,
      config: {
        image_resource: {
          type: "registry-image",
          source: { repository: resourceType.source.repository! },
        },
        outputs: [{ name: resource.name! }],
        run: { path: "/opt/resource/out", args: [`./${resource.name}`] },
      },
      assert: { code: 0 },
      ...hooks,
    },
    JSON.stringify({ source: resource.source, params: step.params }),
    `${pathContext}/put`,
  );

  const version = JSON.parse(putResponse.stdout).version;

  await ctx.runTask(
    {
      task: `get-${resource.name}`,
      config: {
        image_resource: {
          type: "registry-image",
          source: { repository: resourceType.source.repository! },
        },
        outputs: [{ name: resource.name! }],
        run: { path: "/opt/resource/in", args: [`./${resource.name}`] },
      },
      assert: { code: 0 },
      ...hooks,
    },
    JSON.stringify({ source: resource.source, version }),
    `${pathContext}/get`,
  );
}

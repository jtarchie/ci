/// <reference path="../packages/ci/src/global.d.ts" />

// deno-lint-ignore no-unused-vars
function createPipeline(config: PipelineConfig) {
  validatePipelineConfig(config);

  return async () => {
    const knownMounts: { [key: string]: VolumeResult } = {};

    for (const step of config.jobs[0].plan) {
      if ("get" in step) {
        await processGetStep(step, config, knownMounts);
      } else if ("task" in step) {
        await runTask(step, knownMounts);
      }
    }
  };
}

function validatePipelineConfig(config: PipelineConfig): void {
  assert.truthy(
    config.jobs.length > 0,
    "Pipeline must have at least one job",
  );

  assert.truthy(
    config.jobs.every((job) => job.plan.length > 0),
    "Every job must have at least one step",
  );

  if (config.resources.length > 0) {
    validateResources(config);
  }
}

function validateResources(config: PipelineConfig): void {
  assert.truthy(
    config.resources.every((resource) =>
      config.resource_types.some((type) => type.name === resource.type)
    ),
    "Every resource must have a valid resource type",
  );

  assert.truthy(
    config.jobs.every((job) =>
      job.plan.every((step) => {
        if ("get" in step) {
          return config.resources.some((resource) =>
            resource.name === step.get
          );
        }
        return true; // not a resource step, ignore lookup
      })
    ),
    "Every get must have a resource reference",
  );
}

async function processGetStep(
  step: Get,
  config: PipelineConfig,
  knownMounts: { [key: string]: VolumeResult },
): Promise<void> {
  const resource = findResource(config, step.get);
  const resourceType = findResourceType(config, resource?.type);

  const checkResult = await runResourceCheck(
    resource,
    resourceType,
    knownMounts,
  );
  const checkPayload = JSON.parse(checkResult.stdout);
  console.log(checkResult.stdout);

  await runResourceGet(resource, resourceType, checkPayload, knownMounts);
}

function findResource(config: PipelineConfig, resourceName: string) {
  const resource = config.resources.find((resource) =>
    resource.name === resourceName
  );
  return resource!;
}

function findResourceType(config: PipelineConfig, typeName?: string) {
  const resourceType = config.resource_types.find((type) =>
    type.name === typeName
  );
  return resourceType!;
}

async function runResourceCheck(
  resource: Resource,
  resourceType: ResourceType,
  knownMounts: { [key: string]: VolumeResult },
) {
  return await runTask(
    {
      task: `check-${resource?.name}`,
      config: {
        image_resource: {
          type: "registry-image",
          source: {
            repository: resourceType?.source.repository!,
          },
        },
        run: {
          path: "/opt/resource/check",
        },
      },
      assert: {
        code: 0,
      },
    },
    knownMounts,
    JSON.stringify({
      source: resource?.source,
    }),
  );
}

async function runResourceGet(
  resource: Resource,
  resourceType: ResourceType,
  checkPayload: unknown[],
  knownMounts: { [key: string]: VolumeResult },
) {
  return await runTask(
    {
      task: `get-${resource?.name}`,
      config: {
        image_resource: {
          type: "registry-image",
          source: {
            repository: resourceType?.source.repository!,
          },
        },
        outputs: [
          { name: resource?.name! },
        ],
        run: {
          path: "/opt/resource/in",
          args: [`./${resource?.name}`],
        },
      },
      assert: {
        code: 0,
      },
    },
    knownMounts,
    JSON.stringify({
      source: resource?.source,
      version: checkPayload[0],
    }),
  );
}

async function runTask(
  step: Task,
  knownMounts: { [key: string]: VolumeResult },
  stdin?: string,
) {
  const mounts = await prepareMounts(step, knownMounts);

  const result = await runtime.run({
    name: step.task,
    image: step.config.image_resource.source.repository,
    command: [step.config.run.path].concat(step.config.run.args ?? []),
    mounts: mounts,
    stdin: stdin ?? "",
  });

  validateTaskResult(step, result);

  return result;
}

async function prepareMounts(
  step: Task,
  knownMounts: { [key: string]: VolumeResult },
): Promise<{ [key: string]: VolumeResult }> {
  const mounts: { [key: string]: VolumeResult } = {};

  for (const mount of step.config.inputs ?? []) {
    knownMounts[mount.name] ||= await runtime.createVolume();
    mounts[mount.name] = knownMounts[mount.name];
  }

  for (const mount of step.config.outputs ?? []) {
    knownMounts[mount.name] ||= await runtime.createVolume();
    mounts[mount.name] = knownMounts[mount.name];
  }

  return mounts;
}

function validateTaskResult(step: Task, result: RunTaskResult): void {
  if (step.assert.stdout && step.assert.stdout.trim() !== "") {
    assert.containsString(step.assert.stdout, result.stdout);
  }

  if (step.assert.stderr && step.assert.stderr.trim() !== "") {
    assert.containsString(step.assert.stderr, result.stderr);
  }

  if (typeof step.assert.code === "number") {
    assert.equal(step.assert.code, result.code);
  }
}

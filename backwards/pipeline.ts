/// <reference path="../packages/ci/src/global.d.ts" />

// deno-lint-ignore no-unused-vars
function createPipeline(config: PipelineConfig) {
  assert.truthy(
    config.jobs.length > 0,
    "Pipeline must have at least one job",
  );

  assert.truthy(
    config.jobs.every((job) => job.plan.length > 0),
    "Every job must have at least one step",
  );

  if (config.resources.length > 0) {
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

          // not a resource step, ignore lookup
          return true;
        })
      ),
      "Every get must have a resource reference",
    );
  }

  return async () => {
    const knownMounts: { [key: string]: VolumeResult } = {};

    for (const step of config.jobs[0].plan) {
      if ("get" in step) {
        const resource = config.resources.find((resource) =>
          resource.name === step.get
        );
        const resourceType = config.resource_types.find((type) =>
          type.name === resource?.type
        );

        // check
        const checkResult = await runTask(
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

        // get (stdout -> stdin)
        const checkPayload = JSON.parse(checkResult.stdout);
        console.log(checkResult.stdout);

        await runTask(
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

      if ("task" in step) {
        await runTask(step, knownMounts);
      }
    }
  };
}
async function runTask(
  step: Task,
  knownMounts: { [key: string]: VolumeResult },
  stdin?: string,
) {
  const mounts: { [key: string]: VolumeResult } = {};

  for (const mount of step.config.inputs ?? []) {
    knownMounts[mount.name] ||= await runtime.createVolume();
    mounts[mount.name] = knownMounts[mount.name];
  }
  for (const mount of step.config.outputs ?? []) {
    knownMounts[mount.name] ||= await runtime.createVolume();
    mounts[mount.name] = knownMounts[mount.name];
  }

  const result = await runtime.run({
    name: step.task,
    image: step.config.image_resource.source.repository,
    command: [step.config.run.path].concat(step.config.run.args ?? []),
    mounts: mounts,
    stdin: stdin ?? "",
  });

  if (step.assert.stdout && step.assert.stdout.trim() !== "") {
    assert.containsString(step.assert.stdout, result.stdout);
  }
  if (step.assert.stderr && step.assert.stderr.trim() !== "") {
    assert.containsString(step.assert.stderr, result.stderr);
  }
  if (typeof step.assert.code === "number") {
    assert.equal(step.assert.code, result.code);
  }

  return result;
}

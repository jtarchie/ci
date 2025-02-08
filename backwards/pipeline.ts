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

  return async () => {
    const knownMounts: { [key: string]: VolumeResult } = {};

    for (const task of config.jobs[0].plan) {
      const mounts: { [key: string]: VolumeResult } = {};

      for (const mount of task.config.inputs ?? []) {
        knownMounts[mount.name] ||= await runtime.createVolume();
        mounts[mount.name] = knownMounts[mount.name];
      }
      for (const mount of task.config.outputs ?? []) {
        knownMounts[mount.name] ||= await runtime.createVolume();
        mounts[mount.name] = knownMounts[mount.name];
      }

      const result = await runtime.run({
        name: task.task,
        image: task.config.image_resource.source.repository,
        command: [task.config.run.path].concat(task.config.run.args),
        mounts: mounts,
      });

      if (task.assert.stdout && task.assert.stdout.trim() !== "") {
        assert.containsString(task.assert.stdout, result.stdout);
      }
      if (task.assert.stderr && task.assert.stderr.trim() !== "") {
        assert.containsString(task.assert.stderr, result.stderr);
      }
      if (typeof task.assert.code === "number") {
        assert.equal(task.assert.code, result.code);
      }
    }
  };
}

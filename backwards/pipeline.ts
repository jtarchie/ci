/// <reference path="../packages/ci/src/global.d.ts" />

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
    const task = config.jobs[0].plan[0];
    const result = await run({
      name: task.task,
      image: task.config.image_resource.source.repository,
      command: [task.config.run.path].concat(task.config.run.args),
    });

    if (task.assert.stdout != "") {
      assert.containsString(task.assert.stdout, result.stdout);
    }
    if (task.assert.stderr != "") {
      assert.containsString(task.assert.stderr, result.stderr);
    }
    if (task.assert.code !== null) {
      assert.equal(task.assert.code, result.code);
    }
  };
}

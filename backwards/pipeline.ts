/// <reference path="../packages/ci/src/global.d.ts" />

function createPipeline(config) {
  assert.truthy(
    config.jobs.length > 0,
    "Pipeline must have at least one job",
  );

  assert.truthy(
    config.jobs.every((job) => job.steps.length > 0),
    "Every job must have at least one step",
  );

  return () => {
    const task = config.jobs.steps[0];
    run({
      name: task.name,
      image: task.image,
      command: task.command,
    });
  };
}

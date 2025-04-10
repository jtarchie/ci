/// <reference path="../../packages/ci/src/global.d.ts" />

import { PipelineRunner } from "./pipeline_runner.ts";

// Public API function
export function createPipeline(config: PipelineConfig) {
  const runner = new PipelineRunner(config);
  return () => runner.run();
}

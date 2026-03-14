/// <reference path="../../packages/pocketci/src/global.d.ts" />

import { PipelineRunner } from "./pipeline_runner.ts";

// Public API function
export function createPipeline(config: PipelineConfig) {
  const runner = new PipelineRunner(config);
  return () => runner.run();
}

(globalThis as typeof globalThis & {
  createPipeline: typeof createPipeline;
}).createPipeline = createPipeline;

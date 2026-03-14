/// <reference path="../../../packages/pocketci/src/global.d.ts" />

import type { StepContext } from "./step_context.ts";

export interface StepHandler {
  getIdentifier(step: Step): string;
  process(ctx: StepContext, step: Step, pathContext: string): Promise<void>;
}

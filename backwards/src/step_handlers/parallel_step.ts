/// <reference path="../../../packages/pocketci/src/global.d.ts" />

import type { StepContext } from "./step_context.ts";
import type { StepHandler } from "./step_handler.ts";
import { DoStepHandler } from "./do_step.ts";

export class ParallelStepHandler implements StepHandler {
  private doHandler: DoStepHandler;

  getIdentifier(_step: Step): string {
    return "in_parallel";
  }

  constructor(doHandler: DoStepHandler) {
    this.doHandler = doHandler;
  }

  async process(
    ctx: StepContext,
    step: InParallel,
    pathContext: string,
  ): Promise<void> {
    await this.doHandler.process(ctx, step, pathContext);
  }
}

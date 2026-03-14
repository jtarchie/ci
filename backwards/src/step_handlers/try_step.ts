/// <reference path="../../../packages/pocketci/src/global.d.ts" />

import type { StepContext } from "./step_context.ts";
import type { StepHandler } from "./step_handler.ts";
import { DoStepHandler } from "./do_step.ts";

export class TryStepHandler implements StepHandler {
  private doHandler: DoStepHandler;

  getIdentifier(_step: Step): string {
    return "try";
  }

  constructor(doHandler: DoStepHandler) {
    this.doHandler = doHandler;
  }

  async process(
    ctx: StepContext,
    step: Try,
    pathContext: string,
  ): Promise<void> {
    try {
      await this.doHandler.process(ctx, step, pathContext);
    } catch (_err) {
      // do nothing
    } finally {
      storage.set(pathContext, { status: "success" });
    }
  }
}

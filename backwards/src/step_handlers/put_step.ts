/// <reference path="../../../packages/pocketci/src/global.d.ts" />

import type { StepContext } from "./step_context.ts";
import type { StepHandler } from "./step_handler.ts";
import {
  findResource,
  findResourceType,
  stepHooks,
} from "./resource_helpers.ts";

export class PutStepHandler implements StepHandler {
  getIdentifier(step: Step): string {
    return `put/${(step as Put).put}`;
  }

  async process(
    ctx: StepContext,
    step: Put,
    pathContext: string,
  ): Promise<void> {
    const resource = findResource(ctx.resources, step.put);
    const resourceType = findResourceType(ctx.resourceTypes, resource?.type);
    const hooks = stepHooks(step);

    const putResponse = await ctx.runTask(
      {
        task: `put-${resource.name}`,
        config: {
          image_resource: {
            type: "registry-image",
            source: { repository: resourceType.source.repository! },
          },
          outputs: [{ name: resource.name! }],
          run: { path: "/opt/resource/out", args: [`./${resource.name}`] },
        },
        assert: { code: 0 },
        ...hooks,
      },
      JSON.stringify({ source: resource.source, params: step.params }),
      `${pathContext}/put`,
    );

    const version = JSON.parse(putResponse.stdout).version;

    await ctx.runTask(
      {
        task: `get-${resource.name}`,
        config: {
          image_resource: {
            type: "registry-image",
            source: { repository: resourceType.source.repository! },
          },
          outputs: [{ name: resource.name! }],
          run: { path: "/opt/resource/in", args: [`./${resource.name}`] },
        },
        assert: { code: 0 },
        ...hooks,
      },
      JSON.stringify({ source: resource.source, version }),
      `${pathContext}/get`,
    );
  }
}

/// <reference path="../../../packages/pocketci/src/global.d.ts" />

import { TaskFailure } from "../task_runner.ts";
import type { StepContext } from "./step_context.ts";

function elapsedSinceStr(startedAt: string): string {
  const ms = Date.now() - new Date(startedAt).getTime();
  const totalSeconds = Math.floor(ms / 1000);
  const h = Math.floor(totalSeconds / 3600);
  const m = Math.floor((totalSeconds % 3600) / 60);
  const s = totalSeconds % 60;
  if (h > 0) return `${h}h ${m}m ${s}s`;
  if (m > 0) return `${m}m ${s}s`;
  return `${s}s`;
}

export async function processAgentStep(
  ctx: StepContext,
  step: AgentStep,
  pathContext: string,
): Promise<void> {
  const storageKey = `${ctx.paths.getBaseStorageKey()}/${pathContext}`;
  const auditBaseKey =
    `/agent-audit/${ctx.buildID}/jobs/${ctx.jobName}/${pathContext}/events`;

  const image = step.config?.image_resource?.source?.repository ?? "busybox";

  // Collect input and output mounts from earlier get/put steps and volumes.
  const mounts: KnownMounts = {};
  for (const input of (step.config?.inputs ?? [])) {
    const knownMount = ctx.taskRunner.getKnownMounts()[input.name];
    if (knownMount) {
      mounts[input.name] = knownMount;
    }
  }

  const outputs = step.config?.outputs ?? [];
  for (const output of outputs) {
    ctx.taskRunner.getKnownMounts()[output.name] ||= await runtime
      .createVolume({ name: output.name });
    mounts[output.name] = ctx.taskRunner.getKnownMounts()[output.name];
  }

  const outputVolumePath = outputs.length > 0 ? outputs[0].name : "";

  let accumulatedOutput = "";
  let latestUsage: AgentUsage | undefined;
  const auditLog: AuditEvent[] = [];
  const startedAt = new Date().toISOString();

  storage.set(storageKey, { status: "pending", started_at: startedAt });

  // Throttled persistence helpers
  let persistPending = false;
  let lastPersistMs = 0;
  const persistThrottleMs = 500;

  const doPersist = () => {
    persistPending = false;
    lastPersistMs = Date.now();
    storage.set(storageKey, {
      status: "running",
      started_at: startedAt,
      stdout: accumulatedOutput,
      usage: latestUsage,
      audit_log: auditLog,
    });
  };

  const persistRunningState = () => {
    if (Date.now() - lastPersistMs < persistThrottleMs) {
      persistPending = true;
      return;
    }
    doPersist();
  };

  try {
    const result = await runtime.agent({
      name: step.agent,
      prompt: step.prompt,
      model: step.model,
      image,
      mounts,
      outputVolumePath,
      llm: step.llm,
      thinking: step.thinking,
      safety: step.safety,
      context_guard: step.context_guard,
      limits: step.limits,
      context: step.context,
      onUsage: (usage: AgentUsage) => {
        latestUsage = usage;
        persistRunningState();
      },
      onAuditEvent: (event: AuditEvent) => {
        auditLog.push(event);
        storage.set(`${auditBaseKey}/${auditLog.length - 1}`, {
          ...event,
          index: auditLog.length - 1,
        });
        persistRunningState();
      },
      onOutput: (_stream: "stdout" | "stderr", data: string) => {
        accumulatedOutput += data;
        persistRunningState();
      },
    });

    if (persistPending) doPersist();

    storage.set(storageKey, {
      status: result.status === "limit_exceeded" ? "limit_exceeded" : "success",
      started_at: startedAt,
      elapsed: elapsedSinceStr(startedAt),
      stdout: result.text,
      usage: latestUsage ?? result.usage,
      audit_log: result.auditLog,
    });

    for (const output of outputs) {
      ctx.taskRunner.getKnownMounts()[output.name] = mounts[output.name];
    }
  } catch (error) {
    storage.set(storageKey, {
      status: "failure",
      started_at: startedAt,
      elapsed: elapsedSinceStr(startedAt),
      stdout: accumulatedOutput,
      error_message: String(error),
      usage: latestUsage,
      audit_log: auditLog,
    });
    throw new TaskFailure(`Agent ${step.agent} failed: ${error}`);
  }
}

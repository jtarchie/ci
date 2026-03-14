/// <reference path="../../packages/pocketci/src/global.d.ts" />

export function formatElapsed(startedAt: string): string {
  const ms = Date.now() - new Date(startedAt).getTime();
  const totalSeconds = Math.floor(ms / 1000);
  const h = Math.floor(totalSeconds / 3600);
  const m = Math.floor((totalSeconds % 3600) / 60);
  const s = totalSeconds % 60;
  if (h > 0) return `${h}h ${m}m ${s}s`;
  if (m > 0) return `${m}m ${s}s`;
  return `${s}s`;
}

export function safeStorageGet(key: string): unknown {
  try {
    return storage.get(key);
  } catch {
    return null;
  }
}

export function getBuildID(): string {
  return (typeof pipelineContext !== "undefined" && pipelineContext.runID)
    ? pipelineContext.runID
    : String(Date.now());
}

export function extractJobDependencies(plan: Step[]): string[] {
  const dependencies: string[] = [];
  for (const step of plan) {
    if ("get" in step && step.passed) {
      for (const passedJob of step.passed) {
        if (!dependencies.includes(passedJob)) {
          dependencies.push(passedJob);
        }
      }
    }
  }
  return dependencies;
}

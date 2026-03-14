/// <reference path="../../../packages/pocketci/src/global.d.ts" />

import type { TaskRunner } from "../task_runner.ts";
import type { JobConcurrency } from "../job_concurrency.ts";
import type { JobStoragePaths } from "../job_storage_paths.ts";
import type { StepVariableResolver } from "../step_variable_resolver.ts";

export interface StepContext {
  paths: JobStoragePaths;
  concurrency: JobConcurrency;
  variableResolver: StepVariableResolver;
  taskRunner: TaskRunner;
  resources: Resource[];
  resourceTypes: ResourceType[];
  buildID: string;
  jobName: string;
  processStep: (step: Step, pathContext: string) => Promise<void>;
  processStepInternal: (
    step: Step,
    pathContext: string,
    attempt?: number,
  ) => Promise<void>;
  runTask: (
    step: Task,
    stdin?: string,
    pathContext?: string,
  ) => Promise<RunTaskResult>;
}

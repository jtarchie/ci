// types for the pipeline
declare global {
  interface RunTaskConfig {
    name: string;
    image: string;
    command: string[];
  }

  interface RunTaskResult {
    stdout: string;
    stderr: string;
    error: string;
    code: number;
  }

  namespace runtime {
    function run(task: RunTaskConfig): Promise<RunTaskResult>;
  }

  namespace assert {
    function containsElement<T>(
      element: T,
      array: T[],
      message?: string,
    ): void;
    function containsString(
      substr: string,
      str: string,
      message?: string,
    ): void;
    function equal<T>(expected: T, actual: T, message?: string): void;
    function notEqual<T>(expected: T, actual: T, message?: string): void;
    function truthy(value: unknown, message?: string): void;
  }
}

// types for backwards compatibility
declare global {
  interface TaskConfig {
    platform?: string;
    image_resource: {
      type: string;
      source: { [key: string]: string };
    };
    run: {
      path: string;
      args: string[];
    };
  }

  interface Task {
    task: string;
    config: TaskConfig;
    assert: {
      stdout: string;
      stderr: string;
      code: number | null;
    };
  }

  type Step = Task;

  interface Job {
    name: string;
    plan: Step[];
  }

  interface PipelineConfig {
    jobs: Job[];
  }
}

export {};

// types for the pipeline
declare global {
  interface RunTaskConfig {
    command: {
      path: string;
      args?: string[];
      user?: string;
    };
    env?: { [key: string]: string };
    image: string;
    mounts?: KnownMounts;
    name: string;
    privileged?: boolean;
    stdin?: string;
    timeout?: string;
  }

  interface RunTaskResult {
    code: number;
    stderr: string;
    stdout: string;

    status: "complete" | "abort";
    message: string;
  }

  interface VolumeConfig {
    name?: string;
    size?: number;
  }

  interface VolumeResult {
    error: string;
  }

  namespace storage {
    function set(key: string, value: any): Promise<void>;
  }

  namespace runtime {
    function createVolume(volume?: VolumeConfig): Promise<VolumeResult>;
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

  namespace YAML {
    function parse(text: string): object;
    function stringify(obj: object): string;
  }
}

// types for backwards compatibility
declare global {
  interface TaskConfig {
    env?: { [key: string]: string };
    platform?: string;
    image_resource: {
      type: string;
      source: { [key: string]: string };
    };
    inputs?: { name: string }[];
    outputs?: { name: string }[];
    run: {
      path: string;
      args?: string[];
      user?: string;
    };
    params?: { [key: string]: string };
  }

  interface Task {
    task: string;
    config: TaskConfig;
    file?: string;
    privileged?: boolean;

    assert?: {
      stdout?: string;
      stderr?: string;
      code?: number | null;
    };

    ensure?: Step;
    on_abort?: Step;
    on_error?: Step;
    on_success?: Step;
    on_failure?: Step;
    timeout?: string;
  }

  interface Get {
    get: string;
    resource: string;
    params: { [key: string]: string };
    trigger: boolean;
    version: string;
    passed?: string[];

    ensure?: Step;
    on_abort?: Step;
    on_error?: Step;
    on_success?: Step;
    on_failure?: Step;
    timeout?: string;
  }

  interface Put {
    put: string;
    resource: string;
    params: { [key: string]: string };

    ensure?: Step;
    on_abort?: Step;
    on_error?: Step;
    on_success?: Step;
    on_failure?: Step;
    timeout?: string;
  }

  interface Do {
    do: Step[];

    ensure?: Step;
    on_abort?: Step;
    on_error?: Step;
    on_success?: Step;
    on_failure?: Step;
  }

  interface InParallel {
    in_parallel: {
      steps: Step[];
      limit?: number;
      fail_fast?: boolean;
    };

    ensure?: Step;
    on_abort?: Step;
    on_error?: Step;
    on_success?: Step;
    on_failure?: Step;
  }

  interface Try {
    try: Step[];

    ensure?: Step;
    on_abort?: Step;
    on_error?: Step;
    on_success?: Step;
    on_failure?: Step;
  }

  type Step = Task | Get | Put | Do | Try | InParallel;

  interface Job {
    name: string;
    plan: Step[];
    assert: {
      execution?: string[];
    };
  }

  interface Resource {
    name: string;
    type: string;
    source: { [key: string]: string };
  }

  interface ResourceType {
    name: string;
    type: string;
    source: { [key: string]: string };
  }

  interface PipelineConfig {
    assert: {
      execution?: string[];
    };
    jobs: Job[];
    resource_types: ResourceType[];
    resources: Resource[];
  }

  type KnownMounts = Record<string, VolumeResult>;
}

export {};

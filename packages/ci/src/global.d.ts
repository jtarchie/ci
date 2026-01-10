// types for the pipeline
declare global {
  // Common base types
  type SourceConfig = { [key: string]: string };
  type EnvVars = { [key: string]: string };
  type ParamsConfig = { [key: string]: string };

  interface CommandConfig {
    path: string;
    args?: string[];
    user?: string;
  }

  interface ContainerLimits {
    cpu?: number;
    memory?: number;
  }

  interface AssertionBase {
    execution?: string[];
  }

  interface TaskAssertion {
    stdout?: string;
    stderr?: string;
    code?: number | null;
  }

  // Runtime types
  interface RunTaskConfig {
    command: CommandConfig;
    container_limits?: ContainerLimits;
    env?: EnvVars;
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

  type KnownMounts = Record<string, VolumeResult>;

  // Utility namespaces
  namespace storage {
    function set(key: string, value: unknown): Promise<void>;
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

  // Notification types
  interface NotifyConfig {
    type: "slack" | "teams" | "http";
    token?: string; // For Slack
    webhook?: string; // For Teams
    url?: string; // For HTTP
    channels?: string[]; // For Slack
    headers?: Record<string, string>; // For HTTP
    method?: string; // For HTTP (defaults to POST)
    recipients?: string[]; // Generic recipients
  }

  interface NotifyContext {
    pipelineName: string;
    jobName: string;
    buildID: string;
    status: "pending" | "running" | "success" | "failure" | "error";
    startTime: string;
    endTime: string;
    duration: string;
    environment: Record<string, string>;
    taskResults: Record<string, unknown>;
  }

  interface NotifyInput {
    name: string; // Config name
    message: string; // Template message (Go template with Sprig)
    async?: boolean; // Fire-and-forget mode (default: false)
  }

  interface NotifyResult {
    success: boolean;
    error?: string;
  }

  namespace notify {
    function setConfigs(configs: Record<string, NotifyConfig>): void;
    function setContext(ctx: NotifyContext): void;
    function updateStatus(status: string): void;
    function updateJobName(jobName: string): void;
    function send(input: NotifyInput): Promise<NotifyResult>;
    function sendMultiple(
      names: string[],
      message: string,
      async?: boolean,
    ): Promise<NotifyResult>;
  }

  // Native Resources types
  interface ResourceVersion {
    [key: string]: string;
  }

  interface ResourceMetadataField {
    name: string;
    value: string;
  }

  interface ResourceCheckInput {
    type: string;
    source: { [key: string]: unknown };
    version?: ResourceVersion;
  }

  interface ResourceCheckResult {
    versions: ResourceVersion[];
  }

  interface ResourceFetchInput {
    type: string;
    source: { [key: string]: unknown };
    version: ResourceVersion;
    params?: { [key: string]: unknown };
    destDir: string;
  }

  interface ResourceFetchResult {
    version: ResourceVersion;
    metadata: ResourceMetadataField[];
  }

  interface ResourcePushInput {
    type: string;
    source: { [key: string]: unknown };
    params?: { [key: string]: unknown };
    srcDir: string;
  }

  interface ResourcePushResult {
    version: ResourceVersion;
    metadata: ResourceMetadataField[];
  }

  namespace nativeResources {
    function check(input: ResourceCheckInput): ResourceCheckResult;
    function fetch(input: ResourceFetchInput): ResourceFetchResult;
    function push(input: ResourcePushInput): ResourcePushResult;
    function isNative(resourceType: string): boolean;
    function listNativeResources(): string[];
  }
}

// types for backwards compatibility
declare global {
  // Common hook interfaces
  interface StepHooks {
    ensure?: Step;
    on_abort?: Step;
    on_error?: Step;
    on_success?: Step;
    on_failure?: Step;
    timeout?: string;
  }

  // Across modifier type
  interface AcrossVar {
    var: string;
    values: string[];
    max_in_flight?: number;
  }

  // Resource related interfaces
  interface ResourceBase {
    name: string;
    type: string;
    source: SourceConfig;
  }

  interface ImageResource {
    type: string;
    source: SourceConfig;
  }

  interface TaskConfig {
    container_limits?: ContainerLimits;
    env?: EnvVars;
    platform?: string;
    image_resource: ImageResource;
    inputs?: { name: string }[];
    outputs?: { name: string }[];
    run: CommandConfig;
    params?: ParamsConfig;
  }

  // Step interfaces
  interface Task extends StepHooks {
    task: string;
    config: TaskConfig;
    container_limits?: ContainerLimits;
    file?: string;
    privileged?: boolean;
    assert?: TaskAssertion;
    attempts?: number;
    across?: AcrossVar[];
    fail_fast?: boolean;
  }

  interface Get extends StepHooks {
    get: string;
    resource: string;
    params: ParamsConfig;
    trigger: boolean;
    version: string;
    passed?: string[];
    attempts?: number;
    across?: AcrossVar[];
    fail_fast?: boolean;
  }

  interface Put extends StepHooks {
    put: string;
    resource: string;
    params: ParamsConfig;
    attempts?: number;
    across?: AcrossVar[];
    fail_fast?: boolean;
  }

  interface Do extends StepHooks {
    do: Step[];
    attempts?: number;
    across?: AcrossVar[];
    fail_fast?: boolean;
  }

  interface InParallel extends StepHooks {
    in_parallel: {
      steps: Step[];
      limit?: number;
      fail_fast?: boolean;
    };
    attempts?: number;
    across?: AcrossVar[];
  }

  interface Try extends StepHooks {
    try: Step[];
    attempts?: number;
    across?: AcrossVar[];
    fail_fast?: boolean;
  }

  // Notify step for sending notifications
  interface NotifyStep extends StepHooks {
    notify: string | string[]; // Config name(s)
    message: string; // Go template message with Sprig functions
    async?: boolean; // Fire-and-forget mode (default: false)
    attempts?: number;
    across?: AcrossVar[];
    fail_fast?: boolean;
  }

  type Step = Task | Get | Put | Do | Try | InParallel | NotifyStep;

  // Pipeline configuration
  interface Job extends StepHooks {
    name: string;
    plan: Step[];
    assert: AssertionBase;
  }

  interface JobConfig {
    name: string;
    plan: Step[];
    on_success?: Step;
    on_failure?: Step;
    on_error?: Step;
    on_abort?: Step;
    ensure?: Step;
    assert?: {
      execution?: string[];
    };
  }

  type Resource = ResourceBase;
  type ResourceType = ResourceBase;

  interface PipelineConfig {
    assert: AssertionBase;
    jobs: Job[];
    notifications?: Record<string, NotifyConfig>; // Top-level notification configs
    resource_types: ResourceType[];
    resources: Resource[];
  }
}

export {};

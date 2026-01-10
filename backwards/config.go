package backwards

import "time"

// https://github.com/concourse/concourse/blob/master/atc/config.go
type ImageResource struct {
	Source map[string]interface{} `yaml:"source,omitempty"`
	Type   string                 `yaml:"type,omitempty"`
}

type TaskConfigRun struct {
	Args []string `yaml:"args,omitempty"`
	Path string   `validate:"required"   yaml:"path,omitempty"`
	User string   `yaml:"user,omitempty"`
}

type Input struct {
	Name string `validate:"required" yaml:"name,omitempty"`
}

type Output struct {
	Name string `validate:"required" yaml:"name,omitempty"`
}

type Inputs []Input

type Outputs []Output

type ContainerLimits struct {
	CPU    int64 `yaml:"cpu,omitempty"`
	Memory int64 `yaml:"memory,omitempty"`
}

type TaskConfig struct {
	ContainerLimits ContainerLimits   `yaml:"container_limits,omitempty"`
	Env             map[string]string `yaml:"env,omitempty"`
	ImageResource   ImageResource     `yaml:"image_resource,omitempty"`
	Inputs          Inputs            `yaml:"inputs,omitempty"`
	Outputs         Outputs           `yaml:"outputs,omitempty"`
	Platform        string            `validate:"oneof='linux' 'darwin' 'windows'" yaml:"platform,omitempty"`
	Run             TaskConfigRun     `validate:"required"                         yaml:"run,omitempty"`
}

type GetConfig struct {
	Resource string            `yaml:"resource,omitempty"`
	Passed   []string          `yaml:"passed,omitempty"`
	Params   map[string]string `yaml:"params,omitempty"`
	Trigger  bool              `yaml:"trigger,omitempty"`
	Version  string            `yaml:"version,omitempty"`
}

type PutConfig struct {
	Resource  string            `yaml:"resource,omitempty"`
	Params    map[string]string `yaml:"params,omitempty"`
	GetParams map[string]string `yaml:"get_params,omitempty"`
	Inputs    []string          `yaml:"inputs,omitempty"`
	NoGet     bool              `yaml:"no_get,omitempty"`
}

type AcrossVar struct {
	Var          string   `yaml:"var,omitempty"`
	Values       []string `yaml:"values,omitempty"`
	MaxInFlight  int      `yaml:"max_in_flight,omitempty"`
}

type Step struct {
	Assert *struct {
		Code   *int   `yaml:"code,omitempty"`
		Stderr string `yaml:"stderr,omitempty"`
		Stdout string `yaml:"stdout,omitempty"`
	} `yaml:"assert,omitempty"`

	Task            string           `yaml:"task,omitempty"`
	TaskConfig      *TaskConfig      `yaml:"config,omitempty"`
	ContainerLimits *ContainerLimits `yaml:"container_limits,omitempty"`
	File            string           `yaml:"file,omitempty"`
	Privileged      bool             `yaml:"privileged,omitempty"`

	Get       string    `yaml:"get,omitempty"`
	GetConfig GetConfig `yaml:",inline,omitempty"`

	Put       string     `yaml:"put,omitempty"`
	PutConfig *PutConfig `yaml:",inline,omitempty"`

	Do        Steps `yaml:"do,omitempty"`
	Ensure    *Step `yaml:"ensure,omitempty"`
	OnAbort   *Step `yaml:"on_abort,omitempty"`
	OnError   *Step `yaml:"on_error,omitempty"`
	OnSuccess *Step `yaml:"on_success,omitempty"`
	OnFailure *Step `yaml:"on_failure,omitempty"`
	Try       Steps `yaml:"try,omitempty"`

	InParallel struct {
		Steps    Steps `yaml:"steps,omitempty"`
		Limit    int   `yaml:"limit,omitempty"`
		FailFast bool  `yaml:"fail_fast,omitempty"`
	} `yaml:"in_parallel,omitempty"`

	Across []AcrossVar `yaml:"across,omitempty"`
	AcrossFailFast bool `yaml:"fail_fast,omitempty"`

	Attempts int           `yaml:"attempts,omitempty"`
	Timeout  time.Duration `yaml:"timeout,omitempty"`
}

type Steps []Step

type Job struct {
	Assert *struct {
		Execution []string `yaml:"execution,omitempty"`
	} `yaml:"assert,omitempty"`

	Name      string        `validate:"required,min=5"      yaml:"name,omitempty"`
	Plan      Steps         `validate:"required,min=1,dive" yaml:"plan,omitempty"`
	Public    bool          `yaml:"public,omitempty"`
	Ensure    *Step         `yaml:"ensure,omitempty"`
	OnAbort   *Step         `yaml:"on_abort,omitempty"`
	OnError   *Step         `yaml:"on_error,omitempty"`
	OnSuccess *Step         `yaml:"on_success,omitempty"`
	OnFailure *Step         `yaml:"on_failure,omitempty"`
	Timeout   time.Duration `yaml:"timeout,omitempty"`
}

type Jobs []Job

type ResourceType struct {
	Name   string                 `validate:"required"     yaml:"name,omitempty"`
	Source map[string]interface{} `yaml:"source,omitempty"`
	Type   string                 `validate:"required"     yaml:"type,omitempty"`
}

type ResourceTypes []ResourceType

type Resource struct {
	Name   string                 `validate:"required"     yaml:"name,omitempty"`
	Source map[string]interface{} `yaml:"source,omitempty"`
	Type   string                 `validate:"required"     yaml:"type,omitempty"`
}

type Resources []Resource

type Config struct {
	Assert struct {
		Execution []string `yaml:"execution,omitempty"`
	} `yaml:"assert,omitempty"`
	Jobs          Jobs          `validate:"required,min=1,dive" yaml:"jobs"`
	Resources     Resources     `yaml:"resources"`
	ResourceTypes ResourceTypes `yaml:"resource_types"`
}

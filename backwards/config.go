package backwards

// https://github.com/concourse/concourse/blob/master/atc/config.go
type ImageResource struct {
	Source map[string]interface{} `yaml:"source,omitempty"`
	Type   string                 `yaml:"type,omitempty"`
}

type TaskConfigRun struct {
	Args []string `yaml:"args,omitempty"`
	Path string   `validate:"required"   yaml:"path,omitempty"`
}

type Input struct {
	Name string `validate:"required" yaml:"name,omitempty"`
}

type Output struct {
	Name string `validate:"required" yaml:"name,omitempty"`
}

type Inputs []Input

type Outputs []Output

type TaskConfig struct {
	Env           map[string]string `yaml:"env,omitempty"`
	ImageResource ImageResource     `yaml:"image_resource,omitempty"`
	Inputs        Inputs            `yaml:"inputs,omitempty"`
	Outputs       Outputs           `yaml:"outputs,omitempty"`
	Platform      string            `validate:"oneof='linux' 'darwin' 'windows'" yaml:"platform,omitempty"`
	Run           TaskConfigRun     `validate:"required"                         yaml:"run,omitempty"`
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

type Step struct {
	Assert *struct {
		Code   *int   `yaml:"code,omitempty"`
		Stderr string `yaml:"stderr,omitempty"`
		Stdout string `yaml:"stdout,omitempty"`
	} `yaml:"assert,omitempty"`

	Task       string      `yaml:"task,omitempty"`
	TaskConfig *TaskConfig `validate:"required_with=Task" yaml:"config,omitempty"`

	Get       string    `yaml:"get,omitempty"`
	GetConfig GetConfig `validated:"required_with=Get" yaml:",inline,omitempty"`

	Put       string     `yaml:"put,omitempty"`
	PutConfig *PutConfig `validated:"required_with=Put" yaml:",inline,omitempty"`

	Do        Steps `yaml:"do,omitempty"`
	Ensure    *Step `yaml:"ensure,omitempty"`
	OnSuccess *Step `yaml:"on_success,omitempty"`
	OnFailure *Step `yaml:"on_failure,omitempty"`
}

type Steps []Step

type Job struct {
	Assert *struct {
		Execution []string `yaml:"execution,omitempty"`
	} `yaml:"assert,omitempty"`

	Name   string `validate:"required,min=5"      yaml:"name,omitempty"`
	Plan   Steps  `validate:"required,min=1,dive" yaml:"plan,omitempty"`
	Public bool   `yaml:"public,omitempty"`
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
	Jobs          Jobs          `validate:"required,min=1,dive" yaml:"jobs"`
	Resources     Resources     `yaml:"resources"`
	ResourceTypes ResourceTypes `yaml:"resource_types"`
}

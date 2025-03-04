package backwards

import "encoding/json"

// https://github.com/concourse/concourse/blob/master/atc/config.go
type ImageResource struct {
	Source map[string]interface{} `json:"source" yaml:"source"`
	Type   string                 `json:"type"   yaml:"type"`
}

type TaskConfigRun struct {
	Args []string `json:"args" yaml:"args"`
	Path string   `json:"path" validate:"required" yaml:"path"`
}

type Input struct {
	Name string `json:"name" validate:"required" yaml:"name"`
}

type Output struct {
	Name string `json:"name" validate:"required" yaml:"name"`
}

type Inputs []Input

func (i Inputs) MarshalJSON() ([]byte, error) {
	if len(i) == 0 {
		return []byte("[]"), nil
	}

	//nolint: wrapcheck
	return json.Marshal([]Input(i))
}

type Outputs []Output

func (o Outputs) MarshalJSON() ([]byte, error) {
	if len(o) == 0 {
		return []byte("[]"), nil
	}

	//nolint: wrapcheck
	return json.Marshal([]Output(o))
}

type TaskConfig struct {
	Env           map[string]string `json:"env"            yaml:"env"`
	ImageResource ImageResource     `json:"image_resource" yaml:"image_resource"`
	Inputs        Inputs            `json:"inputs"         yaml:"inputs"`
	Outputs       Outputs           `json:"outputs"        yaml:"outputs"`
	Platform      string            `json:"platform"       validate:"oneof='linux' 'darwin' 'windows'" yaml:"platform"`
	Run           TaskConfigRun     `json:"run"            validate:"required"                         yaml:"run"`
}

type GetConfig struct {
	Resource string            `json:"resource" yaml:"resource"`
	Passed   []string          `json:"passed"   yaml:"passed"`
	Params   map[string]string `json:"params"   yaml:"params"`
	Trigger  bool              `json:"trigger"  yaml:"trigger"`
	Version  string            `json:"version"  yaml:"version"`
}

type Step struct {
	Assert struct {
		Code   *int   `json:"code"   yaml:"code"`
		Stderr string `json:"stderr" yaml:"stderr"`
		Stdout string `json:"stdout" yaml:"stdout"`
	} `json:"assert" yaml:"assert"`

	Task       string     `json:"task"   yaml:"task"`
	TaskConfig TaskConfig `json:"config" validate:"required_if=Task ''" yaml:"config"`

	Get       string `json:"get"                     yaml:"get"`
	GetConfig `validated:"required_if=Get ''" yaml:",inline"`
}

type Steps []Step

type Job struct {
	Name   string `json:"name"   validate:"required,min=5"      yaml:"name"`
	Plan   Steps  `json:"plan"   validate:"required,min=1,dive" yaml:"plan"`
	Public bool   `json:"public" yaml:"public"`
}

type Jobs []Job

type ResourceType struct {
	Name   string                 `json:"name"   validate:"required" yaml:"name"`
	Source map[string]interface{} `json:"source" yaml:"source"`
	Type   string                 `json:"type"   validate:"required" yaml:"type"`
}

type ResourceTypes []ResourceType

type Resource struct {
	Name   string                 `json:"name"   validate:"required" yaml:"name"`
	Source map[string]interface{} `json:"source" yaml:"source"`
	Type   string                 `json:"type"   validate:"required" yaml:"type"`
}

type Resources []Resource

type Config struct {
	Jobs          Jobs          `json:"jobs"           validate:"required,min=1,dive" yaml:"jobs"`
	Resources     Resources     `json:"resources"      yaml:"resources"`
	ResourceTypes ResourceTypes `json:"resource_types" yaml:"resource_types"`
}

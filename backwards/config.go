package backwards

// https://github.com/concourse/concourse/blob/master/atc/config.go
type ImageResource struct {
	Type   string                 `json:"type"   yaml:"type"`
	Source map[string]interface{} `json:"source" yaml:"source"`
}

type TaskConfigRun struct {
	Path string   `json:"path" validate:"required" yaml:"path"`
	Args []string `json:"args" yaml:"args"`
}

type TaskConfig struct {
	Platform      string        `json:"platform"       validate:"oneof='linux' 'darwin' 'windows'" yaml:"platform"`
	ImageResource ImageResource `json:"image_resource" yaml:"image_resource"`
	Run           TaskConfigRun `json:"run"            validate:"required"                         yaml:"run"`
}

type Step struct {
	Task   string `json:"task" yaml:"task"`
	Assert struct {
		Stdout string `json:"stdout" yaml:"stdout"`
		Stderr string `json:"stderr" yaml:"stderr"`
		Code   *int   `json:"code"   yaml:"code"`
	} `yaml:"assert" json:"assert"`
	Config TaskConfig `json:"config" validate:"required_with=Task" yaml:"config"`
}

type Steps []Step

type Job struct {
	Name   string `json:"name"   validate:"required,min=5"      yaml:"name"`
	Public bool   `json:"public" yaml:"public"`
	Plan   Steps  `json:"plan"   validate:"required,min=1,dive" yaml:"plan"`
}

type Jobs []Job

type Config struct {
	Jobs Jobs `json:"jobs" validate:"required,min=1,dive" yaml:"jobs"`
}

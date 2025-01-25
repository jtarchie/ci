package backwards

// https://github.com/concourse/concourse/blob/master/atc/config.go
type ImageResource struct {
	Type   string                 `json:"type"   yaml:"type"`
	Source map[string]interface{} `json:"source" yaml:"source"`
}

type TaskConfigRun struct {
	Path string   `json:"path" yaml:"path"`
	Args []string `json:"args" yaml:"args"`
}

type TaskConfig struct {
	Platform      string        `json:"platform"       yaml:"platform"`
	ImageResource ImageResource `json:"image_resource" yaml:"image_resource"`
	Run           TaskConfigRun `json:"run"            yaml:"run"`
}

type Step struct {
	Task   string `json:"task" yaml:"task"`
	Assert struct {
		Stdout string `json:"stdout" yaml:"stdout"`
		Stderr string `json:"stderr" yaml:"stderr"`
		Code   *int   `json:"code"   yaml:"code"`
	} `yaml:"assert" json:"assert"`
	Config TaskConfig `json:"config" yaml:"config"`
}

type Steps []Step

type Job struct {
	Name   string `json:"name"   yaml:"name"`
	Public bool   `json:"public" yaml:"public"`
	Plan   Steps  `json:"plan"   yaml:"plan"`
}

type Jobs []Job

type Config struct {
	Jobs Jobs `json:"jobs" yaml:"jobs"`
}

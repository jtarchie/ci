package backwards

// https://github.com/concourse/concourse/blob/master/atc/config.go
type ImageResource struct {
	Type   string                 `yaml:"type" json:"type"`
	Source map[string]interface{} `yaml:"source" json:"source"`
}

type TaskConfigRun struct {
	Path string   `yaml:"path" json:"path"`
	Args []string `yaml:"args" json:"args"`
}

type TaskConfig struct {
	Platform      string        `yaml:"platform" json:"platform"`
	ImageResource ImageResource `yaml:"image_resource" json:"image_resource"`
	Run           TaskConfigRun `yaml:"run" json:"run"`
}

type Step struct {
	Task   string `yaml:"task" json:"task"`
	Assert struct {
		Stdout string `yaml:"stdout" json:"stdout"`
		Stderr string `yaml:"stderr" json:"stderr"`
		Code   *int   `yaml:"code" json:"code"`
	} `yaml:"assert" json:"assert"`
	Config TaskConfig `yaml:"config" json:"config"`
}

type Steps []Step

type Job struct {
	Name   string `yaml:"name" json:"name"`
	Public bool   `yaml:"public" json:"public"`
	Plan   Steps  `yaml:"plan" json:"plan"`
}

type Jobs []Job

type Config struct {
	Jobs Jobs `yaml:"jobs" json:"jobs"`
}

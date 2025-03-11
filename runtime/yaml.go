package runtime

import (
	"log/slog"

	"github.com/goccy/go-yaml"
)

type YAML struct {
	logger *slog.Logger
}

func NewYAML(logger *slog.Logger) *YAML {
	return &YAML{
		logger: logger.WithGroup("yaml"),
	}
}

func (y *YAML) Parse(source string) interface{} {
	var payload interface{}

	_ = yaml.Unmarshal([]byte(source), &payload)

	return payload
}

func (y *YAML) Stringify(payload interface{}) string {
	contents, err := yaml.Marshal(payload)
	if err != nil {
		return ""
	}

	return string(contents)
}

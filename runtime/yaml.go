package runtime

import (
	"log/slog"

	"github.com/dop251/goja"
	"github.com/goccy/go-yaml"
)

type YAML struct {
	logger *slog.Logger
	vm     *goja.Runtime
}

func NewYAML(vm *goja.Runtime, logger *slog.Logger) *YAML {
	return &YAML{
		logger: logger.WithGroup("yaml"),
		vm:     vm,
	}
}

func (y *YAML) Parse(source string) interface{} {
	var payload interface{}

	err := yaml.Unmarshal([]byte(source), &payload)
	if err != nil {
		y.logger.Debug("yaml.parse", "err", err)
		y.vm.Interrupt(err)

		return nil
	}

	return payload
}

func (y *YAML) Stringify(payload interface{}) string {
	contents, err := yaml.Marshal(payload)
	if err != nil {
		return ""
	}

	return string(contents)
}

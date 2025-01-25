package backwards

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"

	"gopkg.in/yaml.v3"
)

//go:embed pipeline.ts
var pipelineJS string

func NewPipeline(filename string) (string, error) {
	var config Config

	contents, err := os.ReadFile(filename)
	if err != nil {
		return "", fmt.Errorf("could not read pipeline: %w", err)
	}

	err = yaml.Unmarshal(contents, &config)
	if err != nil {
		return "", fmt.Errorf("could not unmarshal pipeline: %w", err)
	}

	contents, err = json.Marshal(config)
	if err != nil {
		return "", fmt.Errorf("could not marshal pipeline: %w", err)
	}

	slog.Info("pipeline", "contents", string(contents))
	pipeline := "const config = " + string(contents) + ";\n" +
		pipelineJS +
		"\n; const pipeline = createPipeline(config); export { pipeline };"

	return pipeline, nil
}

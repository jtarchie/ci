package backwards

import (
	_ "embed"
	"fmt"
	"os"

	"github.com/go-playground/validator/v10"
	"github.com/goccy/go-yaml"
)

//go:generate go run github.com/evanw/esbuild/... --tree-shaking=true --platform=neutral --bundle --outfile=bundle.js src/index.ts
//go:embed bundle.js
var pipelineJS string

func NewPipeline(filename string) (string, error) {
	var config Config

	contents, err := os.ReadFile(filename)
	if err != nil {
		return "", fmt.Errorf("could not read pipeline: %w", err)
	}

	err = yaml.UnmarshalWithOptions(contents, &config, yaml.Strict())
	if err != nil {
		return "", fmt.Errorf("could not unmarshal pipeline: %w", err)
	}

	validate := validator.New(validator.WithRequiredStructEnabled())

	err = validate.Struct(config)
	if err != nil {
		return "", fmt.Errorf("could not validate pipeline: %w", err)
	}

	contents, err = yaml.MarshalWithOptions(config, yaml.JSON())
	if err != nil {
		return "", fmt.Errorf("could not marshal pipeline: %w", err)
	}

	pipeline := "const config = " + string(contents) + ";\n" +
		pipelineJS +
		"\n; const pipeline = createPipeline(config); export { pipeline };"

	return pipeline, nil
}

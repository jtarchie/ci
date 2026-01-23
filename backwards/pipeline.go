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

	err = yaml.Unmarshal(contents, &config)
	if err != nil {
		return "", fmt.Errorf("could not unmarshal pipeline: %w", err)
	}

	validate := validator.New(validator.WithRequiredStructEnabled())

	err = validate.Struct(config)
	if err != nil {
		return "", fmt.Errorf("could not validate pipeline: %w", err)
	}

	if err := validateResourceTypes(&config); err != nil {
		return "", err
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

// validateResourceTypes checks that every resource references a defined resource type.
// The "registry-image" type is built-in and always available.
func validateResourceTypes(config *Config) error {
	// Build a set of valid resource type names
	validTypes := make(map[string]bool)
	validTypes["registry-image"] = true // Built-in type

	for _, rt := range config.ResourceTypes {
		validTypes[rt.Name] = true
	}

	// Check each resource has a valid type
	for _, resource := range config.Resources {
		if !validTypes[resource.Type] {
			return fmt.Errorf("resource %q has undefined resource type %q", resource.Name, resource.Type)
		}
	}

	return nil
}

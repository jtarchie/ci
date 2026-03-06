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
	contents, err := os.ReadFile(filename)
	if err != nil {
		return "", fmt.Errorf("could not read pipeline: %w", err)
	}

	return NewPipelineFromContent(string(contents))
}

// NewPipelineFromContent transpiles a YAML pipeline string into a TypeScript
// pipeline definition that can be executed by the JS runtime. Unlike NewPipeline
// it accepts content directly instead of reading from a file.
func NewPipelineFromContent(content string) (string, error) {
	var config Config

	err := yaml.Unmarshal([]byte(content), &config)
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

	if err := validateSteps(config.Jobs); err != nil {
		return "", err
	}

	jsonBytes, err := yaml.MarshalWithOptions(config, yaml.JSON())
	if err != nil {
		return "", fmt.Errorf("could not marshal pipeline: %w", err)
	}

	pipeline := "const config = " + string(jsonBytes) + ";\n" +
		pipelineJS +
		"\n; const pipeline = createPipeline(config); export { pipeline };"

	return pipeline, nil
}

// ValidatePipeline validates that the given YAML content is a well-formed
// pipeline definition without producing any output. It is suitable for early
// error checking at set-pipeline time without performing transpilation.
func ValidatePipeline(content []byte) error {
	var config Config

	if err := yaml.Unmarshal(content, &config); err != nil {
		return fmt.Errorf("could not unmarshal pipeline: %w", err)
	}

	validate := validator.New(validator.WithRequiredStructEnabled())

	if err := validate.Struct(config); err != nil {
		return fmt.Errorf("could not validate pipeline: %w", err)
	}

	if err := validateResourceTypes(&config); err != nil {
		return err
	}

	if err := validateSteps(config.Jobs); err != nil {
		return err
	}

	return nil
}

// validateSteps checks that task steps have a required run.path field (unless using file:).
func validateSteps(jobs Jobs) error {
	for _, job := range jobs {
		for i, step := range job.Plan {
			if step.Task != "" && step.File == "" {
				if step.TaskConfig == nil || step.TaskConfig.Run == nil || step.TaskConfig.Run.Path == "" {
					return fmt.Errorf("task step %q in job %q (index %d) requires config.run.path", step.Task, job.Name, i)
				}
			}
		}
	}

	return nil
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

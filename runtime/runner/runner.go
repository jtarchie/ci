package runner

// Runner is the interface for running pipeline steps.
// Both PipelineRunner and ResumableRunner implement this interface.
type Runner interface {
	Run(input RunInput) (*RunResult, error)
	CreateVolume(input VolumeInput) (*VolumeResult, error)
	CleanupVolumes() error
	// StartSandbox starts a long-lived sandbox container for multi-command execution.
	// Returns an error if the underlying driver does not support sandbox mode.
	StartSandbox(input SandboxInput) (*SandboxHandle, error)
}

// Ensure both runners implement the interface.
var (
	_ Runner = (*PipelineRunner)(nil)
	_ Runner = (*ResumableRunner)(nil)
)

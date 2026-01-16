package runtime

// Runner is the interface for running pipeline steps.
// Both PipelineRunner and ResumableRunner implement this interface.
type Runner interface {
	Run(input RunInput) (*RunResult, error)
	CreateVolume(input VolumeInput) (*VolumeResult, error)
	CleanupVolumes() error
}

// Ensure both runners implement the interface.
var (
	_ Runner = (*PipelineRunner)(nil)
	_ Runner = (*ResumableRunner)(nil)
)

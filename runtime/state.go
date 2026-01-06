package runtime

import "time"

// StepStatus represents the execution status of a pipeline step.
type StepStatus string

const (
	// StepStatusPending indicates the step has not started yet.
	StepStatusPending StepStatus = "pending"
	// StepStatusRunning indicates the step is currently executing.
	StepStatusRunning StepStatus = "running"
	// StepStatusCompleted indicates the step finished successfully.
	StepStatusCompleted StepStatus = "completed"
	// StepStatusFailed indicates the step finished with an error.
	StepStatusFailed StepStatus = "failed"
	// StepStatusAborted indicates the step was interrupted/aborted.
	StepStatusAborted StepStatus = "aborted"
)

// StepState represents the persisted state of a pipeline step.
type StepState struct {
	// StepID is a unique identifier for this step within the pipeline run.
	StepID string `json:"step_id"`
	// Name is the human-readable name of the step.
	Name string `json:"name"`
	// Status is the current execution status.
	Status StepStatus `json:"status"`
	// ContainerID is the driver-specific container identifier (for reattachment).
	ContainerID string `json:"container_id,omitempty"`
	// TaskID is the task identifier used by the orchestrator.
	TaskID string `json:"task_id,omitempty"`
	// StartedAt is when the step started executing.
	StartedAt *time.Time `json:"started_at,omitempty"`
	// CompletedAt is when the step finished (successfully or not).
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	// ExitCode is the container exit code (if completed).
	ExitCode *int `json:"exit_code,omitempty"`
	// Result stores the serialized step result for completed steps.
	Result *RunResult `json:"result,omitempty"`
	// Error stores the error message if the step failed.
	Error string `json:"error,omitempty"`
}

// IsTerminal returns true if the step is in a terminal state (completed, failed, or aborted).
func (s *StepState) IsTerminal() bool {
	switch s.Status {
	case StepStatusCompleted, StepStatusFailed, StepStatusAborted:
		return true
	default:
		return false
	}
}

// IsResumable returns true if the step can be resumed (running state with a container ID).
func (s *StepState) IsResumable() bool {
	return s.Status == StepStatusRunning && s.ContainerID != ""
}

// CanSkip returns true if the step was already completed successfully and can be skipped on resume.
func (s *StepState) CanSkip() bool {
	return s.Status == StepStatusCompleted && s.Result != nil
}

// PipelineState represents the persisted state of an entire pipeline run.
type PipelineState struct {
	// RunID is a unique identifier for this pipeline run.
	RunID string `json:"run_id"`
	// Steps contains the state of each step, keyed by step ID.
	Steps map[string]*StepState `json:"steps"`
	// StepOrder maintains the order in which steps were created.
	StepOrder []string `json:"step_order"`
	// StartedAt is when the pipeline run started.
	StartedAt *time.Time `json:"started_at,omitempty"`
	// CompletedAt is when the pipeline run finished.
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	// ResumeEnabled indicates if resumability was enabled for this run.
	ResumeEnabled bool `json:"resume_enabled"`
}

// NewPipelineState creates a new pipeline state for a run.
func NewPipelineState(runID string, resumeEnabled bool) *PipelineState {
	now := time.Now()
	return &PipelineState{
		RunID:         runID,
		Steps:         make(map[string]*StepState),
		StepOrder:     make([]string, 0),
		StartedAt:     &now,
		ResumeEnabled: resumeEnabled,
	}
}

// GetStep returns the step state for a given step ID, or nil if not found.
func (p *PipelineState) GetStep(stepID string) *StepState {
	return p.Steps[stepID]
}

// SetStep adds or updates a step state.
func (p *PipelineState) SetStep(state *StepState) {
	if _, exists := p.Steps[state.StepID]; !exists {
		p.StepOrder = append(p.StepOrder, state.StepID)
	}
	p.Steps[state.StepID] = state
}

// LastStep returns the last step in execution order, or nil if no steps.
func (p *PipelineState) LastStep() *StepState {
	if len(p.StepOrder) == 0 {
		return nil
	}
	return p.Steps[p.StepOrder[len(p.StepOrder)-1]]
}

// InProgressSteps returns all steps that are currently running.
func (p *PipelineState) InProgressSteps() []*StepState {
	var result []*StepState
	for _, stepID := range p.StepOrder {
		if step := p.Steps[stepID]; step.Status == StepStatusRunning {
			result = append(result, step)
		}
	}
	return result
}

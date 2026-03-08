package runtime

// This file provides type aliases and function aliases for backward compatibility.
// External consumers of the runtime package can continue using these names
// while the actual implementations live in sub-packages.

import (
	"github.com/jtarchie/pocketci/runtime/agent"
	"github.com/jtarchie/pocketci/runtime/jsapi"
	"github.com/jtarchie/pocketci/runtime/runner"
	"github.com/jtarchie/pocketci/runtime/support"
)

// === Type aliases: support ===

// === Type aliases: runner ===

type (
	Runner          = runner.Runner
	PipelineRunner  = runner.PipelineRunner
	ResumableRunner = runner.ResumableRunner
	ResumeOptions   = runner.ResumeOptions
	ResourceRunner  = runner.ResourceRunner
	RunInput        = runner.RunInput
	RunResult       = runner.RunResult
	RunStatus       = runner.RunStatus
	VolumeInput     = runner.VolumeInput
	VolumeResult    = runner.VolumeResult
	SandboxInput    = runner.SandboxInput
	SandboxHandle   = runner.SandboxHandle
	ExecInput       = runner.ExecInput
	OutputCallback  = runner.OutputCallback
	PipelineState   = runner.PipelineState
	StepState       = runner.StepState
	StepStatus      = runner.StepStatus

	ResourceCheckInput  = runner.ResourceCheckInput
	ResourceCheckResult = runner.ResourceCheckResult
	ResourceFetchInput  = runner.ResourceFetchInput
	ResourceFetchResult = runner.ResourceFetchResult
	ResourcePushInput   = runner.ResourcePushInput
	ResourcePushResult  = runner.ResourcePushResult
	NativeResourceInfo  = runner.NativeResourceInfo
)

// === Type aliases: jsapi ===

type (
	Assert        = jsapi.Assert
	YAML          = jsapi.YAML
	FetchRuntime  = jsapi.FetchRuntime
	FetchResponse = jsapi.FetchResponse
	HTTPRuntime   = jsapi.HTTPRuntime
	WebhookData   = jsapi.WebhookData
	HTTPResponse  = jsapi.HTTPResponse
	Notifier      = jsapi.Notifier
	NotifyConfig  = jsapi.NotifyConfig
	NotifyContext = jsapi.NotifyContext
	NotifyInput   = jsapi.NotifyInput
	NotifyResult  = jsapi.NotifyResult
	NotifyRuntime = jsapi.NotifyRuntime
	Printer       = jsapi.Printer
)

// === Type aliases: agent ===

type (
	AgentConfig             = agent.AgentConfig
	AgentResult             = agent.AgentResult
	AgentUsage              = agent.AgentUsage
	AgentLLMConfig          = agent.AgentLLMConfig
	AgentThinkingConfig     = agent.AgentThinkingConfig
	AgentContextGuardConfig = agent.AgentContextGuardConfig
	AgentContext            = agent.AgentContext
	AgentContextTask        = agent.AgentContextTask
	AuditEvent              = agent.AuditEvent
	AuditUsage              = agent.AuditUsage
	ToolCallRecord          = agent.ToolCallRecord
)

// === Function/constructor aliases ===

var (
	// support
	UniqueID              = support.UniqueID
	DeterministicTaskID   = support.DeterministicTaskID
	PipelineID            = support.PipelineID
	DeterministicVolumeID = support.DeterministicVolumeID
	RedactSecrets         = support.RedactSecrets

	// runner
	NewPipelineRunner  = runner.NewPipelineRunner
	NewResumableRunner = runner.NewResumableRunner
	NewResourceRunner  = runner.NewResourceRunner
	NewPipelineState   = runner.NewPipelineState

	// jsapi
	NewAssert        = jsapi.NewAssert
	NewYAML          = jsapi.NewYAML
	NewFetchRuntime  = jsapi.NewFetchRuntime
	NewHTTPRuntime   = jsapi.NewHTTPRuntime
	NewNotifier      = jsapi.NewNotifier
	NewNotifyRuntime = jsapi.NewNotifyRuntime
	NewPrinter       = jsapi.NewPrinter

	// agent
	RunAgent = agent.RunAgent
)

// === Constant aliases ===

const (
	RunAbort    = runner.RunAbort
	RunComplete = runner.RunComplete

	StepStatusPending   = runner.StepStatusPending
	StepStatusRunning   = runner.StepStatusRunning
	StepStatusCompleted = runner.StepStatusCompleted
	StepStatusFailed    = runner.StepStatusFailed
	StepStatusAborted   = runner.StepStatusAborted
)

package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jtarchie/pocketci/orchestra/docker"
	pipelinerunner "github.com/jtarchie/pocketci/runtime/runner"
	"github.com/jtarchie/pocketci/storage"
	. "github.com/onsi/gomega"
)

type fakeStorage struct {
	data map[string]storage.Payload
}

func newFakeStorage() *fakeStorage {
	return &fakeStorage{data: map[string]storage.Payload{}}
}

func (f *fakeStorage) Close() error { return nil }

func (f *fakeStorage) Set(_ context.Context, prefix string, payload any) error {
	if p, ok := payload.(storage.Payload); ok {
		f.data[prefix] = p

		return nil
	}

	if p, ok := payload.(map[string]any); ok {
		f.data[prefix] = storage.Payload(p)

		return nil
	}

	return fmt.Errorf("unsupported payload type %T", payload)
}

func (f *fakeStorage) Get(_ context.Context, prefix string) (storage.Payload, error) {
	p, ok := f.data[prefix]
	if !ok {
		return nil, storage.ErrNotFound
	}

	return p, nil
}

func (f *fakeStorage) GetAll(_ context.Context, prefix string, fields []string) (storage.Results, error) {
	var paths []string
	for p := range f.data {
		if strings.HasPrefix(p, prefix) {
			paths = append(paths, p)
		}
	}

	sort.Strings(paths)

	results := make(storage.Results, 0, len(paths))
	for i, p := range paths {
		payload := f.data[p]
		if len(fields) > 0 {
			filtered := storage.Payload{}
			for _, field := range fields {
				if v, ok := payload[field]; ok {
					filtered[field] = v
				}
			}
			payload = filtered
		}

		results = append(results, storage.Result{
			ID:      i + 1,
			Path:    p,
			Payload: payload,
		})
	}

	return results, nil
}

func (f *fakeStorage) UpdateStatusForPrefix(_ context.Context, _ string, _ []string, _ string) error {
	return nil
}

func (f *fakeStorage) SavePipeline(_ context.Context, _ string, _ string, _ string, _ string) (*storage.Pipeline, error) {
	return nil, fmt.Errorf("not implemented")
}

func (f *fakeStorage) GetPipeline(_ context.Context, _ string) (*storage.Pipeline, error) {
	return nil, fmt.Errorf("not implemented")
}

func (f *fakeStorage) GetPipelineByName(_ context.Context, _ string) (*storage.Pipeline, error) {
	return nil, fmt.Errorf("not implemented")
}

func (f *fakeStorage) DeletePipeline(_ context.Context, _ string) error {
	return fmt.Errorf("not implemented")
}

func (f *fakeStorage) SaveRun(_ context.Context, _ string) (*storage.PipelineRun, error) {
	return nil, fmt.Errorf("not implemented")
}

func (f *fakeStorage) GetRun(_ context.Context, _ string) (*storage.PipelineRun, error) {
	return nil, fmt.Errorf("not implemented")
}

func (f *fakeStorage) SearchRunsByPipeline(_ context.Context, _, _ string, _, _ int) (*storage.PaginationResult[storage.PipelineRun], error) {
	return nil, fmt.Errorf("not implemented")
}

func (f *fakeStorage) UpdateRunStatus(_ context.Context, _ string, _ storage.RunStatus, _ string) error {
	return fmt.Errorf("not implemented")
}

func (f *fakeStorage) SearchPipelines(_ context.Context, _ string, _, _ int) (*storage.PaginationResult[storage.Pipeline], error) {
	return nil, fmt.Errorf("not implemented")
}

func (f *fakeStorage) Search(_ context.Context, _, _ string) (storage.Results, error) {
	return nil, fmt.Errorf("not implemented")
}

func (f *fakeStorage) UpdatePipelineResumeEnabled(_ context.Context, _ string, _ bool) error {
	return fmt.Errorf("not implemented")
}

func (f *fakeStorage) GetRunsByStatus(_ context.Context, _ storage.RunStatus) ([]storage.PipelineRun, error) {
	return nil, fmt.Errorf("not implemented")
}

func newSequencedLLMServer(t *testing.T, responses []string) (*httptest.Server, *int32) {
	t.Helper()

	var reqCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		n := int(atomic.AddInt32(&reqCount, 1))
		idx := n - 1
		if idx >= len(responses) {
			idx = len(responses) - 1
		}

		_, _ = w.Write([]byte(responses[idx]))
	}))

	t.Cleanup(server.Close)

	return server, &reqCount
}

func configureFakeOpenAI(t *testing.T, baseURL string) {
	t.Helper()

	origBaseURL := defaultBaseURLs["openai"]
	defaultBaseURLs["openai"] = baseURL + "/v1"
	t.Cleanup(func() { defaultBaseURLs["openai"] = origBaseURL })
	t.Setenv("OPENAI_API_KEY", "test-key")
}

func newDockerRunner(t *testing.T, prefix string) *pipelinerunner.PipelineRunner {
	t.Helper()

	logger := slog.Default()
	namespace := fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
	runID := prefix + "-run"

	driver, err := docker.NewDocker(namespace, logger, nil)
	if err != nil {
		t.Fatalf("new docker driver: %v", err)
	}

	t.Cleanup(func() { _ = driver.Close() })

	runner := pipelinerunner.NewPipelineRunner(context.Background(), driver, nil, logger, namespace, runID)
	t.Cleanup(func() { _ = runner.CleanupVolumes() })

	return runner
}

func mustCreateVolume(t *testing.T, runner *pipelinerunner.PipelineRunner, name string) pipelinerunner.VolumeResult {
	t.Helper()

	vol, err := runner.CreateVolume(pipelinerunner.VolumeInput{Name: name})
	if err != nil {
		t.Fatalf("create volume %q: %v", name, err)
	}

	return *vol
}

func mustRun(t *testing.T, runner *pipelinerunner.PipelineRunner, input pipelinerunner.RunInput) *pipelinerunner.RunResult {
	t.Helper()

	result, err := runner.Run(input)
	if err != nil {
		t.Fatalf("run %q: %v", input.Name, err)
	}

	return result
}

func seedDiffVolume(t *testing.T, runner *pipelinerunner.PipelineRunner, diffVol pipelinerunner.VolumeResult) {
	t.Helper()

	result := mustRun(t, runner, pipelinerunner.RunInput{
		Name:  "seed-diff",
		Image: "busybox",
		Mounts: map[string]pipelinerunner.VolumeResult{
			"diff": diffVol,
		},
		Command: struct {
			Path string   `json:"path"`
			Args []string `json:"args"`
			User string   `json:"user"`
		}{
			Path: "sh",
			Args: []string{"-c", "printf 'diff --git a/a b/b\\n+added line\\n' > diff/pr.diff"},
		},
	})

	if result.Code != 0 {
		t.Fatalf("seed diff failed with exit code %d: %s", result.Code, result.Stderr)
	}
}

func readResultArtifact(t *testing.T, runner *pipelinerunner.PipelineRunner, outputVol pipelinerunner.VolumeResult, taskName string) map[string]string {
	t.Helper()

	result := mustRun(t, runner, pipelinerunner.RunInput{
		Name:  taskName,
		Image: "busybox",
		Mounts: map[string]pipelinerunner.VolumeResult{
			"final-review": outputVol,
		},
		Command: struct {
			Path string   `json:"path"`
			Args []string `json:"args"`
			User string   `json:"user"`
		}{
			Path: "cat",
			Args: []string{"final-review/result.json"},
		},
	})

	if result.Code != 0 {
		t.Fatalf("read result artifact failed with exit code %d: %s", result.Code, result.Stderr)
	}

	var artifact map[string]string
	if err := json.Unmarshal([]byte(result.Stdout), &artifact); err != nil {
		t.Fatalf("unmarshal result artifact: %v", err)
	}

	return artifact
}

func TestRunAgent_FakeLLM_RealDocker(t *testing.T) {
	assert := NewGomegaWithT(t)

	responses := []string{
		`{
			"id":"chatcmpl-1",
			"object":"chat.completion",
			"created":1730000000,
			"model":"fake-model",
			"choices":[{
				"index":0,
				"message":{
					"role":"assistant",
					"content":"",
					"tool_calls":[{
						"id":"call_ls",
						"type":"function",
						"function":{
							"name":"run_command",
							"arguments":"{\"command\":\"/bin/sh\",\"args\":[\"-c\",\"ls diff\"]}"
						}
					}]
				},
				"finish_reason":"tool_calls"
			}],
			"usage":{"prompt_tokens":20,"completion_tokens":5,"total_tokens":25}
		}`,
		`{
			"id":"chatcmpl-2",
			"object":"chat.completion",
			"created":1730000001,
			"model":"fake-model",
			"choices":[{
				"index":0,
				"message":{
					"role":"assistant",
					"content":"",
					"tool_calls":[{
						"id":"call_cat",
						"type":"function",
						"function":{
							"name":"run_command",
							"arguments":"{\"command\":\"/bin/sh\",\"args\":[\"-c\",\"cat diff/pr.diff\"]}"
						}
					}]
				},
				"finish_reason":"tool_calls"
			}],
			"usage":{"prompt_tokens":30,"completion_tokens":6,"total_tokens":36}
		}`,
		`{
			"id":"chatcmpl-3",
			"object":"chat.completion",
			"created":1730000002,
			"model":"fake-model",
			"choices":[{
				"index":0,
				"message":{
					"role":"assistant",
					"content":"## Code Review\n\n### Summary\nFound diff file and successfully read content."
				},
				"finish_reason":"stop"
			}],
			"usage":{"prompt_tokens":40,"completion_tokens":10,"total_tokens":50}
		}`,
	}

	llm, reqCount := newSequencedLLMServer(t, responses)
	configureFakeOpenAI(t, llm.URL)

	runner := newDockerRunner(t, "agent-int")
	diffVol := mustCreateVolume(t, runner, "diff")
	outVol := mustCreateVolume(t, runner, "final-review")
	seedDiffVolume(t, runner, diffVol)

	result, err := RunAgent(context.Background(), runner, nil, "", AgentConfig{
		Name:   "final-reviewer",
		Prompt: "Use run_command to verify diff file via ls and cat, then summarize.",
		Model:  "openai/fake-model",
		Image:  "busybox",
		Mounts: map[string]pipelinerunner.VolumeResult{
			"diff":         diffVol,
			"final-review": outVol,
		},
		// Intentionally pass host-like path to verify path resolution logic.
		OutputVolumePath: outVol.Path,
	})
	assert.Expect(err).NotTo(HaveOccurred())
	assert.Expect(result).NotTo(BeNil())
	assert.Expect(result.Text).To(ContainSubstring("Found diff file"))
	assert.Expect(atomic.LoadInt32(reqCount)).To(BeNumerically(">=", 3))

	var sawLS bool
	var sawCat bool
	for _, event := range result.AuditLog {
		if event.Type != "tool_response" || event.ToolName != "run_command" || event.ToolResult == nil {
			continue
		}

		stdout, _ := event.ToolResult["stdout"].(string)
		if strings.Contains(stdout, "pr.diff") {
			sawLS = true
		}
		if strings.Contains(stdout, "added line") {
			sawCat = true
		}
	}

	if !sawLS || !sawCat {
		auditJSON, _ := json.MarshalIndent(result.AuditLog, "", "  ")
		t.Fatalf("expected ls/cat evidence in tool responses (sawLS=%v sawCat=%v)\nAuditLog:\n%s", sawLS, sawCat, string(auditJSON))
	}

	assert.Expect(sawLS).To(BeTrue())
	assert.Expect(sawCat).To(BeTrue())

	artifact := readResultArtifact(t, runner, outVol, "read-result")
	assert.Expect(artifact["status"]).To(Equal("success"))
	assert.Expect(artifact["text"]).To(ContainSubstring("Found diff file"))
}

// TestRunAgent_FakeLLM_RunScript_RealDocker verifies that the run_script tool
// executes a multi-line script in a single round-trip and that the audit log
// records one tool_call (not two) even though two commands run in the script.
func TestRunAgent_FakeLLM_RunScript_RealDocker(t *testing.T) {
	assert := NewGomegaWithT(t)

	responses := []string{
		// Turn 1: agent calls run_script with a two-step script.
		`{
			"id":"chatcmpl-rs-1",
			"object":"chat.completion",
			"created":1730000100,
			"model":"fake-model",
			"choices":[{
				"index":0,
				"message":{
					"role":"assistant",
					"content":"",
					"tool_calls":[{
						"id":"call_script",
						"type":"function",
						"function":{
							"name":"run_script",
							"arguments":"{\"script\":\"set -e\\nls diff\\ncat diff/pr.diff\"}"
						}
					}]
				},
				"finish_reason":"tool_calls"
			}],
			"usage":{"prompt_tokens":20,"completion_tokens":5,"total_tokens":25}
		}`,
		// Turn 2: agent summarizes after receiving the combined output.
		`{
			"id":"chatcmpl-rs-2",
			"object":"chat.completion",
			"created":1730000101,
			"model":"fake-model",
			"choices":[{
				"index":0,
				"message":{
					"role":"assistant",
					"content":"Script ran successfully: found diff and read content in one call."
				},
				"finish_reason":"stop"
			}],
			"usage":{"prompt_tokens":30,"completion_tokens":8,"total_tokens":38}
		}`,
	}

	llm, reqCount := newSequencedLLMServer(t, responses)
	configureFakeOpenAI(t, llm.URL)

	runner := newDockerRunner(t, "agent-script")
	diffVol := mustCreateVolume(t, runner, "diff")
	outVol := mustCreateVolume(t, runner, "final-review")
	seedDiffVolume(t, runner, diffVol)

	result, err := RunAgent(context.Background(), runner, nil, "", AgentConfig{
		Name:   "script-agent",
		Prompt: "Use run_script to list and read diff/pr.diff in one call, then summarize.",
		Model:  "openai/fake-model",
		Image:  "busybox",
		Mounts: map[string]pipelinerunner.VolumeResult{
			"diff":         diffVol,
			"final-review": outVol,
		},
		OutputVolumePath: outVol.Path,
	})
	assert.Expect(err).NotTo(HaveOccurred())
	assert.Expect(result).NotTo(BeNil())
	assert.Expect(result.Text).To(ContainSubstring("one call"))

	// Exactly two LLM requests: one tool call, one final answer.
	assert.Expect(atomic.LoadInt32(reqCount)).To(BeNumerically("==", 2))

	// Audit log must show exactly one run_script tool_call.
	var scriptCalls int
	var combinedOutput string
	for _, event := range result.AuditLog {
		if event.Type == "tool_call" && event.ToolName == "run_script" {
			scriptCalls++
		}
		if event.Type == "tool_response" && event.ToolName == "run_script" && event.ToolResult != nil {
			combinedOutput, _ = event.ToolResult["stdout"].(string)
		}
	}

	assert.Expect(scriptCalls).To(Equal(1), "expected exactly one run_script tool call")
	// Both ls output and diff content must appear in the single response.
	assert.Expect(combinedOutput).To(ContainSubstring("pr.diff"))
	assert.Expect(combinedOutput).To(ContainSubstring("added line"))

	artifact := readResultArtifact(t, runner, outVol, "read-script-result")
	assert.Expect(artifact["status"]).To(Equal("success"))
}

func TestRunAgent_FakeLLM_InvalidToolArgs_RealDocker(t *testing.T) {
	assert := NewGomegaWithT(t)

	responses := []string{
		`{
			"id":"chatcmpl-invalid",
			"object":"chat.completion",
			"created":1730000010,
			"model":"fake-model",
			"choices":[{
				"index":0,
				"message":{
					"role":"assistant",
					"content":"",
					"tool_calls":[{
						"id":"call_invalid",
						"type":"function",
						"function":{
							"name":"run_command",
							"arguments":"{\"args\":[\"-c\",\"ls\"]}"
						}
					}]
				},
				"finish_reason":"tool_calls"
			}],
			"usage":{"prompt_tokens":20,"completion_tokens":5,"total_tokens":25}
		}`,
		`{
			"id":"chatcmpl-invalid-final",
			"object":"chat.completion",
			"created":1730000011,
			"model":"fake-model",
			"choices":[{
				"index":0,
				"message":{
					"role":"assistant",
					"content":"Tool arguments were invalid, but audit captured the error."
				},
				"finish_reason":"stop"
			}],
			"usage":{"prompt_tokens":25,"completion_tokens":8,"total_tokens":33}
		}`,
	}

	llm, _ := newSequencedLLMServer(t, responses)
	configureFakeOpenAI(t, llm.URL)

	runner := newDockerRunner(t, "agent-int-invalid")
	outVol := mustCreateVolume(t, runner, "final-review")

	result, err := RunAgent(context.Background(), runner, nil, "", AgentConfig{
		Name:   "final-reviewer",
		Prompt: "Try to run ls and summarize the result.",
		Model:  "openai/fake-model",
		Image:  "busybox",
		Mounts: map[string]pipelinerunner.VolumeResult{
			"final-review": outVol,
		},
		OutputVolumePath: outVol.Path,
	})
	assert.Expect(err).NotTo(HaveOccurred())
	assert.Expect(result).NotTo(BeNil())

	var validationErr string
	for _, event := range result.AuditLog {
		if event.Type != "tool_response" || event.ToolName != "run_command" || event.ToolResult == nil {
			continue
		}

		if errText, ok := event.ToolResult["error"].(string); ok {
			validationErr = errText

			break
		}
	}

	assert.Expect(validationErr).To(ContainSubstring("missing properties"))

	artifact := readResultArtifact(t, runner, outVol, "read-invalid-result")
	assert.Expect(artifact["status"]).To(Equal("success"))
	assert.Expect(strings.TrimSpace(artifact["text"])).NotTo(BeEmpty())
}

func TestRunAgent_ContextFilesPreInjection_RealDocker(t *testing.T) {
	assert := NewGomegaWithT(t)

	// The agent receives the file pre-injected — it should answer in one turn
	// without calling any tool.
	responses := []string{
		`{
			"id":"chatcmpl-cf-1",
			"object":"chat.completion",
			"created":1730000200,
			"model":"fake-model",
			"choices":[{
				"index":0,
				"message":{
					"role":"assistant",
					"content":"The diff shows: added line"
				},
				"finish_reason":"stop"
			}],
			"usage":{"prompt_tokens":50,"completion_tokens":10,"total_tokens":60}
		}`,
	}

	llm, reqCount := newSequencedLLMServer(t, responses)
	configureFakeOpenAI(t, llm.URL)

	runner := newDockerRunner(t, "agent-ctx-files")
	diffVol := mustCreateVolume(t, runner, "diff")
	outVol := mustCreateVolume(t, runner, "final-review")
	seedDiffVolume(t, runner, diffVol)

	result, err := RunAgent(context.Background(), runner, nil, "", AgentConfig{
		Name:   "file-context-agent",
		Prompt: "Summarize the diff content already injected in your context.",
		Model:  "openai/fake-model",
		Image:  "busybox",
		Mounts: map[string]pipelinerunner.VolumeResult{
			"diff":         diffVol,
			"final-review": outVol,
		},
		OutputVolumePath: outVol.Path,
		Context: &AgentContext{
			Files: []AgentContextFile{
				{Path: "diff/pr.diff"},
			},
		},
	})
	assert.Expect(err).NotTo(HaveOccurred())
	assert.Expect(result).NotTo(BeNil())
	assert.Expect(result.Text).To(ContainSubstring("added line"))

	// Exactly one LLM request — the file was pre-injected, no tool call needed.
	assert.Expect(atomic.LoadInt32(reqCount)).To(BeNumerically("==", 1))

	// Audit log must contain a pre_context read_file event with the diff content.
	var preContextEvent *AuditEvent
	for i := range result.AuditLog {
		if result.AuditLog[i].Type == "pre_context" && result.AuditLog[i].ToolName == "read_file" {
			preContextEvent = &result.AuditLog[i]
			break
		}
	}

	assert.Expect(preContextEvent).NotTo(BeNil(), "expected a pre_context read_file audit event")
	assert.Expect(preContextEvent.ToolArgs).To(HaveKeyWithValue("path", "diff/pr.diff"))
	content, _ := preContextEvent.ToolResult["content"].(string)
	assert.Expect(content).To(ContainSubstring("added line"))

	// Zero tool calls — no run_command, no run_script, no explicit read_file.
	assert.Expect(result.Usage.ToolCallCount).To(BeZero())

	// Pre-injections must not count against LLM turn budget: they are
	// AppendEvent calls that never produce UsageMetadata, so turnCount
	// (which drives the maxTurns limit) stays at 1 for this single real turn.
	assert.Expect(result.Usage.LLMRequests).To(BeNumerically("==", 1))
}

// TestRunAgent_ContextFilesLLMReceivesContent_RealDocker captures the raw request
// body sent to the LLM and asserts that the pre-injected diff content is present
// and that the user prompt appears before the file content in conversation order.
func TestRunAgent_ContextFilesLLMReceivesContent_RealDocker(t *testing.T) {
	assert := NewGomegaWithT(t)

	var (
		mu          sync.Mutex
		capturedReq string
	)

	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		capturedReq = string(body)
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"chatcmpl-cap-1",
			"object":"chat.completion",
			"created":1730000300,
			"model":"fake-model",
			"choices":[{
				"index":0,
				"message":{"role":"assistant","content":"Diff reviewed."},
				"finish_reason":"stop"
			}],
			"usage":{"prompt_tokens":80,"completion_tokens":5,"total_tokens":85}
		}`))
	}))
	t.Cleanup(llm.Close)
	configureFakeOpenAI(t, llm.URL)

	runner := newDockerRunner(t, "agent-cf-llm")
	diffVol := mustCreateVolume(t, runner, "diff")
	outVol := mustCreateVolume(t, runner, "final-review")
	seedDiffVolume(t, runner, diffVol)

	_, err := RunAgent(context.Background(), runner, nil, "", AgentConfig{
		Name:   "cf-llm-agent",
		Prompt: "Summarize the diff.",
		Model:  "openai/fake-model",
		Image:  "busybox",
		Mounts: map[string]pipelinerunner.VolumeResult{
			"diff":         diffVol,
			"final-review": outVol,
		},
		OutputVolumePath: outVol.Path,
		Context: &AgentContext{
			Files: []AgentContextFile{
				{Path: "diff/pr.diff"},
			},
		},
	})
	assert.Expect(err).NotTo(HaveOccurred())

	mu.Lock()
	body := capturedReq
	mu.Unlock()

	// The first (and only) LLM request must contain the pre-injected file content.
	assert.Expect(body).To(ContainSubstring("added line"),
		"LLM request must contain the pre-injected diff content")

	// The user prompt must precede the injected content so the model sees:
	// user: "Summarize the diff." → model: [read_file call] → user: [result with diff]
	promptIdx := strings.Index(body, "Summarize the diff.")
	fileIdx := strings.Index(body, "added line")
	assert.Expect(promptIdx).To(BeNumerically(">", -1), "user prompt missing from LLM request")
	assert.Expect(promptIdx).To(BeNumerically("<", fileIdx),
		"user prompt must appear before pre-injected file content in LLM messages")
}

func TestRunAgent_ContextTasksPreInjection_RealDocker(t *testing.T) {
	assert := NewGomegaWithT(t)

	responses := []string{
		`{
			"id":"chatcmpl-context",
			"object":"chat.completion",
			"created":1730000020,
			"model":"fake-model",
			"choices":[{
				"index":0,
				"message":{
					"role":"assistant",
					"content":"Used pre-injected context successfully."
				},
				"finish_reason":"stop"
			}],
			"usage":{"prompt_tokens":30,"completion_tokens":7,"total_tokens":37}
		}`,
	}

	llm, _ := newSequencedLLMServer(t, responses)
	configureFakeOpenAI(t, llm.URL)

	runner := newDockerRunner(t, "agent-int-context")
	outVol := mustCreateVolume(t, runner, "final-review")

	st := newFakeStorage()
	runID := "context-run"
	base := "/pipeline/" + runID + "/jobs/review-pr"

	_ = st.Set(context.Background(), base+"/1/agent/code-quality-reviewer", storage.Payload{
		"status": "success",
		"stdout": "- cq issue",
	})
	_ = st.Set(context.Background(), base+"/2/agent/security-reviewer", storage.Payload{
		"status": "success",
		"stdout": "- sec issue",
	})
	_ = st.Set(context.Background(), base+"/3/agent/maintainability-reviewer", storage.Payload{
		"status": "success",
		"stdout": "- maint issue",
	})

	result, err := RunAgent(context.Background(), runner, nil, "", AgentConfig{
		Name:   "final-reviewer",
		Prompt: "Summarize prior reviews.",
		Model:  "openai/fake-model",
		Image:  "busybox",
		Mounts: map[string]pipelinerunner.VolumeResult{
			"final-review": outVol,
		},
		OutputVolumePath: outVol.Path,
		Storage:          st,
		RunID:            runID,
		Context: &AgentContext{
			Tasks: []AgentContextTask{
				{Name: "code-quality-reviewer", Field: "stdout"},
				{Name: "security-reviewer", Field: "stdout"},
				{Name: "maintainability-reviewer", Field: "stdout"},
			},
		},
	})
	assert.Expect(err).NotTo(HaveOccurred())
	assert.Expect(result).NotTo(BeNil())

	var preContextListTasks bool
	var injectedTaskCount int
	for _, event := range result.AuditLog {
		if event.Type != "pre_context" {
			continue
		}

		if event.ToolName == "list_tasks" {
			preContextListTasks = true
		}

		if event.ToolName == "get_task_result" {
			injectedTaskCount++
		}
	}

	assert.Expect(preContextListTasks).To(BeTrue())
	assert.Expect(injectedTaskCount).To(Equal(3))

	artifact := readResultArtifact(t, runner, outVol, "read-context-result")
	assert.Expect(artifact["status"]).To(Equal("success"))
	assert.Expect(strings.TrimSpace(artifact["text"])).NotTo(BeEmpty())
}

func TestRunAgent_WritesResultArtifactForDownstreamTask_RealDocker(t *testing.T) {
	assert := NewGomegaWithT(t)

	responses := []string{
		`{
			"id":"chatcmpl-artifact",
			"object":"chat.completion",
			"created":1730000030,
			"model":"fake-model",
			"choices":[{
				"index":0,
				"message":{
					"role":"assistant",
					"content":"Final synthesized review from the agent."
				},
				"finish_reason":"stop"
			}],
			"usage":{"prompt_tokens":12,"completion_tokens":6,"total_tokens":18}
		}`,
	}

	llm, _ := newSequencedLLMServer(t, responses)
	configureFakeOpenAI(t, llm.URL)

	runner := newDockerRunner(t, "agent-int-artifact")
	outVol := mustCreateVolume(t, runner, "final-review")

	result, err := RunAgent(context.Background(), runner, nil, "", AgentConfig{
		Name:   "final-reviewer",
		Prompt: "Write one concise final review paragraph.",
		Model:  "openai/fake-model",
		Image:  "busybox",
		Mounts: map[string]pipelinerunner.VolumeResult{
			"final-review": outVol,
		},
		OutputVolumePath: outVol.Path,
	})
	assert.Expect(err).NotTo(HaveOccurred())
	assert.Expect(result).NotTo(BeNil())
	assert.Expect(result.Text).To(ContainSubstring("Final synthesized review"))

	artifact := readResultArtifact(t, runner, outVol, "read-downstream-artifact")
	assert.Expect(artifact["status"]).To(Equal("success"))
	assert.Expect(strings.TrimSpace(artifact["text"])).To(ContainSubstring("Final synthesized review"))
}

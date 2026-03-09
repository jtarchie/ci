package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"testing"

	"github.com/jtarchie/pocketci/storage"
	_ "github.com/jtarchie/pocketci/storage/sqlite"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	. "github.com/onsi/gomega"
)

// setupMCPSession creates an in-process MCP client session backed by the given
// storage driver. The caller is responsible for closing the store.
func setupMCPSession(t *testing.T, store storage.Driver) *mcp.ClientSession {
	t.Helper()
	assert := NewWithT(t)

	mcpServer := buildMCPServer(store)
	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	ctx := context.Background()

	ss, err := mcpServer.Connect(ctx, serverTransport, nil)
	assert.Expect(err).NotTo(HaveOccurred())
	t.Cleanup(func() { _ = ss.Close() })

	c := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "1.0"}, nil)
	session, err := c.Connect(ctx, clientTransport, nil)
	assert.Expect(err).NotTo(HaveOccurred())
	t.Cleanup(func() { _ = session.Close() })

	return session
}

// newTestStore creates a temporary SQLite-backed storage driver for testing.
func newTestStore(t *testing.T) storage.Driver {
	t.Helper()
	assert := NewWithT(t)

	buildFile, err := os.CreateTemp(t.TempDir(), "")
	assert.Expect(err).NotTo(HaveOccurred())
	t.Cleanup(func() { _ = buildFile.Close() })

	initStorage, found := storage.GetFromDSN("sqlite://" + buildFile.Name())
	assert.Expect(found).To(BeTrue())

	store, err := initStorage("sqlite://"+buildFile.Name(), "namespace", slog.Default())
	assert.Expect(err).NotTo(HaveOccurred())
	t.Cleanup(func() { _ = store.Close() })

	return store
}

func TestMCPListTools(t *testing.T) {
	t.Parallel()

	store := newTestStore(t)
	session := setupMCPSession(t, store)

	result, err := session.ListTools(context.Background(), nil)
	assert := NewWithT(t)
	assert.Expect(err).NotTo(HaveOccurred())
	assert.Expect(result.Tools).To(HaveLen(4))

	names := make([]string, len(result.Tools))
	for i, tool := range result.Tools {
		names[i] = tool.Name
	}
	assert.Expect(names).To(ConsistOf("get_run", "list_run_tasks", "search_tasks", "search_pipelines"))
}

func TestMCPGetRun(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newTestStore(t)

	pipeline, err := store.SavePipeline(ctx, "test-pipeline", "export const pipeline = async () => {};", "native://", "")
	NewWithT(t).Expect(err).NotTo(HaveOccurred())

	run, err := store.SaveRun(ctx, pipeline.ID)
	NewWithT(t).Expect(err).NotTo(HaveOccurred())

	session := setupMCPSession(t, store)

	t.Run("returns run details for a valid run ID", func(t *testing.T) {
		t.Parallel()
		assert := NewWithT(t)

		result, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name:      "get_run",
			Arguments: map[string]any{"run_id": run.ID},
		})
		assert.Expect(err).NotTo(HaveOccurred())
		assert.Expect(result.IsError).To(BeFalse())
		assert.Expect(result.Content).To(HaveLen(1))

		text := result.Content[0].(*mcp.TextContent).Text
		var gotRun storage.PipelineRun
		assert.Expect(json.Unmarshal([]byte(text), &gotRun)).NotTo(HaveOccurred())
		assert.Expect(gotRun.ID).To(Equal(run.ID))
		assert.Expect(gotRun.PipelineID).To(Equal(pipeline.ID))
		assert.Expect(string(gotRun.Status)).To(Equal("queued"))
	})

	t.Run("returns error for a non-existent run ID", func(t *testing.T) {
		t.Parallel()
		assert := NewWithT(t)

		result, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name:      "get_run",
			Arguments: map[string]any{"run_id": "no-such-run"},
		})
		assert.Expect(err).NotTo(HaveOccurred())
		// Errors from tool handlers are embedded as IsError=true per the MCP spec.
		assert.Expect(result.IsError).To(BeTrue())
	})
}

func TestMCPListRunTasks(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newTestStore(t)
	assert := NewWithT(t)

	pipeline, err := store.SavePipeline(ctx, "tasks-pipeline", "export const pipeline = async () => {};", "native://", "")
	assert.Expect(err).NotTo(HaveOccurred())

	run, err := store.SaveRun(ctx, pipeline.ID)
	assert.Expect(err).NotTo(HaveOccurred())

	// Write two task payloads under the run's path prefix.
	err = store.Set(ctx, "/pipeline/"+run.ID+"/tasks/echo", map[string]any{
		"status": "success",
		"stdout": "hello world",
		"stderr": "",
		"type":   "task",
	})
	assert.Expect(err).NotTo(HaveOccurred())

	err = store.Set(ctx, "/pipeline/"+run.ID+"/tasks/build", map[string]any{
		"status": "failed",
		"stdout": "",
		"stderr": "build failed: exit 1",
		"type":   "task",
	})
	assert.Expect(err).NotTo(HaveOccurred())

	session := setupMCPSession(t, store)

	result, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "list_run_tasks",
		Arguments: map[string]any{"run_id": run.ID},
	})
	assert.Expect(err).NotTo(HaveOccurred())
	assert.Expect(result.IsError).To(BeFalse())
	assert.Expect(result.Content).To(HaveLen(1))

	text := result.Content[0].(*mcp.TextContent).Text
	var tasks storage.Results
	assert.Expect(json.Unmarshal([]byte(text), &tasks)).NotTo(HaveOccurred())
	assert.Expect(tasks).To(HaveLen(2))
}

func TestMCPSearchTasks(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newTestStore(t)
	assert := NewWithT(t)

	pipeline, err := store.SavePipeline(ctx, "search-pipeline", "export const pipeline = async () => {};", "native://", "")
	assert.Expect(err).NotTo(HaveOccurred())

	run, err := store.SaveRun(ctx, pipeline.ID)
	assert.Expect(err).NotTo(HaveOccurred())

	err = store.Set(ctx, "/pipeline/"+run.ID+"/tasks/echo", map[string]any{
		"status": "success",
		"stdout": "unique-token-xyz hello",
	})
	assert.Expect(err).NotTo(HaveOccurred())

	err = store.Set(ctx, "/pipeline/"+run.ID+"/tasks/other", map[string]any{
		"status": "success",
		"stdout": "something else entirely",
	})
	assert.Expect(err).NotTo(HaveOccurred())

	session := setupMCPSession(t, store)

	t.Run("run_id mode searches task output within a run", func(t *testing.T) {
		t.Parallel()
		assert := NewWithT(t)

		result, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name: "search_tasks",
			Arguments: map[string]any{
				"run_id": run.ID,
				"query":  "unique-token-xyz",
			},
		})
		assert.Expect(err).NotTo(HaveOccurred())
		assert.Expect(result.IsError).To(BeFalse())

		text := result.Content[0].(*mcp.TextContent).Text
		var tasks storage.Results
		assert.Expect(json.Unmarshal([]byte(text), &tasks)).NotTo(HaveOccurred())
		assert.Expect(tasks).To(HaveLen(1))
	})

	t.Run("pipeline_id mode searches runs for a pipeline", func(t *testing.T) {
		t.Parallel()
		assert := NewWithT(t)

		// create a second run with a distinct error so it shows up in search
		run2, err := store.SaveRun(ctx, pipeline.ID)
		assert.Expect(err).NotTo(HaveOccurred())
		err = store.UpdateRunStatus(ctx, run2.ID, storage.RunStatusFailed, "unique-pipeline-error-token")
		assert.Expect(err).NotTo(HaveOccurred())

		result, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name: "search_tasks",
			Arguments: map[string]any{
				"pipeline_id": pipeline.ID,
				"query":       "unique-pipeline-error-token",
			},
		})
		assert.Expect(err).NotTo(HaveOccurred())
		assert.Expect(result.IsError).To(BeFalse())

		text := result.Content[0].(*mcp.TextContent).Text
		var page storage.PaginationResult[storage.PipelineRun]
		assert.Expect(json.Unmarshal([]byte(text), &page)).NotTo(HaveOccurred())
		assert.Expect(page.Items).To(HaveLen(1))
		assert.Expect(page.Items[0].ID).To(Equal(run2.ID))
	})

	t.Run("returns error when neither run_id nor pipeline_id is provided", func(t *testing.T) {
		t.Parallel()
		assert := NewWithT(t)

		result, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name:      "search_tasks",
			Arguments: map[string]any{"query": "anything"},
		})
		assert.Expect(err).NotTo(HaveOccurred())
		assert.Expect(result.IsError).To(BeTrue())
	})
}

func TestMCPSearchPipelines(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := newTestStore(t)
	root := NewWithT(t)

	_, err := store.SavePipeline(ctx, "alpha-pipeline", "export const pipeline = async () => {};", "native://", "")
	root.Expect(err).NotTo(HaveOccurred())
	_, err = store.SavePipeline(ctx, "beta-pipeline", "export const pipeline = async () => {};", "native://", "")
	root.Expect(err).NotTo(HaveOccurred())

	session := setupMCPSession(t, store)

	t.Run("empty query returns all pipelines", func(t *testing.T) {
		t.Parallel()
		assert := NewWithT(t)

		result, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name:      "search_pipelines",
			Arguments: map[string]any{"query": ""},
		})
		assert.Expect(err).NotTo(HaveOccurred())
		assert.Expect(result.IsError).To(BeFalse())

		text := result.Content[0].(*mcp.TextContent).Text
		var page storage.PaginationResult[storage.Pipeline]
		assert.Expect(json.Unmarshal([]byte(text), &page)).NotTo(HaveOccurred())
		assert.Expect(page.TotalItems).To(Equal(2))
	})

	t.Run("name query returns matching pipeline", func(t *testing.T) {
		t.Parallel()
		assert := NewWithT(t)

		result, err := session.CallTool(ctx, &mcp.CallToolParams{
			Name:      "search_pipelines",
			Arguments: map[string]any{"query": "alpha"},
		})
		assert.Expect(err).NotTo(HaveOccurred())
		assert.Expect(result.IsError).To(BeFalse())

		text := result.Content[0].(*mcp.TextContent).Text
		var page storage.PaginationResult[storage.Pipeline]
		assert.Expect(json.Unmarshal([]byte(text), &page)).NotTo(HaveOccurred())
		assert.Expect(page.TotalItems).To(Equal(1))
		assert.Expect(page.Items[0].Name).To(Equal("alpha-pipeline"))
	})
}

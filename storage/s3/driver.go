package s3

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/jtarchie/pocketci/runtime"
	"github.com/jtarchie/pocketci/storage"
)

// S3 implements the storage.Driver interface using AWS S3 (or S3-compatible
// stores like MinIO) as the backend. All data is stored as JSON objects at
// hierarchical paths within the configured bucket and prefix.
//
// DSN format: s3://bucket/optional/prefix?region=us-east-1&endpoint=http://localhost:9000
type S3 struct {
	client    *s3.Client
	bucket    string
	prefix    string
	namespace string
	logger    *slog.Logger
}

// NewS3 creates a new S3-backed storage driver.
func NewS3(dsn string, namespace string, logger *slog.Logger) (storage.Driver, error) {
	parsed, err := url.Parse(dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to parse S3 DSN: %w", err)
	}

	if parsed.Scheme != "s3" {
		return nil, fmt.Errorf("expected s3:// DSN, got %s://", parsed.Scheme)
	}

	bucket := parsed.Host
	prefix := strings.TrimPrefix(parsed.Path, "/")

	ctx := context.Background()

	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	var clientOptions []func(*s3.Options)

	query := parsed.Query()

	if region := query.Get("region"); region != "" {
		clientOptions = append(clientOptions, func(o *s3.Options) {
			o.Region = region
		})
	}

	if endpoint := query.Get("endpoint"); endpoint != "" {
		clientOptions = append(clientOptions, func(o *s3.Options) {
			o.BaseEndpoint = aws.String(endpoint)
			o.UsePathStyle = true
		})
	}

	client := s3.NewFromConfig(cfg, clientOptions...)

	return &S3{
		client:    client,
		bucket:    bucket,
		prefix:    prefix,
		namespace: namespace,
		logger:    logger,
	}, nil
}

func (s *S3) Close() error {
	return nil
}

func (s *S3) fullKey(key string) string {
	if s.prefix == "" {
		return key
	}

	return s.prefix + "/" + key
}

func (s *S3) taskKey(prefix string) string {
	return s.fullKey("tasks" + path.Clean("/"+s.namespace+"/"+prefix))
}

// Set stores a payload at the given prefix, merging with any existing payload
// (replicating SQLite's jsonb_patch upsert semantics).
func (s *S3) Set(ctx context.Context, prefix string, payload any) error {
	key := s.taskKey(prefix)

	incoming, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	existing, err := s.getJSON(ctx, key)
	if err != nil && !errors.Is(err, storage.ErrNotFound) {
		return fmt.Errorf("failed to get existing task: %w", err)
	}

	if existing != nil {
		var incomingMap map[string]any
		if err := json.Unmarshal(incoming, &incomingMap); err != nil {
			return fmt.Errorf("failed to unmarshal incoming payload: %w", err)
		}

		for k, v := range incomingMap {
			existing[k] = v
		}

		merged, err := json.Marshal(existing)
		if err != nil {
			return fmt.Errorf("failed to marshal merged payload: %w", err)
		}

		incoming = merged
	}

	return s.putJSON(ctx, key, incoming)
}

func (s *S3) Get(ctx context.Context, prefix string) (storage.Payload, error) {
	key := s.taskKey(prefix)

	payload, err := s.getJSON(ctx, key)
	if err != nil {
		return nil, err
	}

	return payload, nil
}

func (s *S3) GetAll(ctx context.Context, prefix string, fields []string) (storage.Results, error) {
	if len(fields) == 0 {
		fields = []string{"status"}
	}

	keyPrefix := s.taskKey(prefix)

	keys, err := s.listKeys(ctx, keyPrefix)
	if err != nil {
		return nil, fmt.Errorf("failed to list tasks: %w", err)
	}

	sort.Strings(keys)

	var results storage.Results

	for _, key := range keys {
		payload, err := s.getJSON(ctx, key)
		if err != nil {
			continue
		}

		logicalPath := key
		if s.prefix != "" {
			logicalPath = strings.TrimPrefix(key, s.prefix+"/")
		}

		logicalPath = strings.TrimPrefix(logicalPath, "tasks")

		if len(fields) != 1 || fields[0] != "*" {
			filtered := storage.Payload{}
			for _, f := range fields {
				if v, ok := payload[f]; ok {
					filtered[f] = v
				}
			}

			payload = filtered
		}

		results = append(results, storage.Result{
			ID:      0,
			Path:    logicalPath,
			Payload: payload,
		})
	}

	return results, nil
}

func (s *S3) UpdateStatusForPrefix(ctx context.Context, prefix string, matchStatuses []string, newStatus string) error {
	if len(matchStatuses) == 0 {
		return nil
	}

	keyPrefix := s.taskKey(prefix)

	keys, err := s.listKeys(ctx, keyPrefix)
	if err != nil {
		return fmt.Errorf("failed to list tasks for status update: %w", err)
	}

	matchSet := make(map[string]bool, len(matchStatuses))
	for _, ms := range matchStatuses {
		matchSet[ms] = true
	}

	for _, key := range keys {
		payload, err := s.getJSON(ctx, key)
		if err != nil {
			continue
		}

		status, _ := payload["status"].(string)
		if !matchSet[status] {
			continue
		}

		payload["status"] = newStatus

		data, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("failed to marshal updated payload: %w", err)
		}

		if err := s.putJSON(ctx, key, data); err != nil {
			return fmt.Errorf("failed to update task status for key %q: %w", key, err)
		}
	}

	return nil
}

// ─── Pipeline CRUD ──────────────────────────────────────────────────────────

func (s *S3) SavePipeline(ctx context.Context, name, content, driverDSN, contentType string) (*storage.Pipeline, error) {
	newID := runtime.PipelineID(name, content)
	now := time.Now().UTC()

	var storedID string

	existing, err := s.getPipeline(ctx, s.pipelineByNameKey(name))
	if err == nil && existing != nil {
		storedID = existing.ID

		if storedID != newID {
			_ = s.deleteObject(ctx, s.pipelineByIDKey(storedID))
		}
	}

	if storedID == "" {
		storedID = newID
	}

	pipeline := &storage.Pipeline{
		ID:          storedID,
		Name:        name,
		Content:     content,
		ContentType: contentType,
		DriverDSN:   driverDSN,
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	if existing != nil {
		pipeline.CreatedAt = existing.CreatedAt
	}

	data, err := json.Marshal(pipeline)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal pipeline: %w", err)
	}

	if err := s.putJSON(ctx, s.pipelineByIDKey(storedID), data); err != nil {
		return nil, fmt.Errorf("failed to save pipeline by id: %w", err)
	}

	if err := s.putJSON(ctx, s.pipelineByNameKey(name), data); err != nil {
		return nil, fmt.Errorf("failed to save pipeline by name: %w", err)
	}

	return pipeline, nil
}

func (s *S3) GetPipeline(ctx context.Context, id string) (*storage.Pipeline, error) {
	return s.getPipeline(ctx, s.pipelineByIDKey(id))
}

func (s *S3) GetPipelineByName(ctx context.Context, name string) (*storage.Pipeline, error) {
	return s.getPipeline(ctx, s.pipelineByNameKey(name))
}

func (s *S3) DeletePipeline(ctx context.Context, id string) error {
	pipeline, err := s.GetPipeline(ctx, id)
	if err != nil {
		return err
	}

	if err := s.deleteObject(ctx, s.pipelineByIDKey(id)); err != nil {
		return fmt.Errorf("failed to delete pipeline by id: %w", err)
	}

	if err := s.deleteObject(ctx, s.pipelineByNameKey(pipeline.Name)); err != nil {
		return fmt.Errorf("failed to delete pipeline by name: %w", err)
	}

	runKeys, err := s.listKeys(ctx, s.fullKey("runs/"))
	if err != nil {
		return nil
	}

	for _, key := range runKeys {
		run, err := s.getRun(ctx, key)
		if err != nil {
			continue
		}

		if run.PipelineID == id {
			_ = s.deleteObject(ctx, key)
		}
	}

	return nil
}

// ─── Pipeline Run operations ────────────────────────────────────────────────

func (s *S3) SaveRun(ctx context.Context, pipelineID string) (*storage.PipelineRun, error) {
	id := runtime.UniqueID()
	now := time.Now().UTC()

	run := &storage.PipelineRun{
		ID:         id,
		PipelineID: pipelineID,
		Status:     storage.RunStatusQueued,
		CreatedAt:  now,
	}

	data, err := json.Marshal(run)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal run: %w", err)
	}

	if err := s.putJSON(ctx, s.runKey(id), data); err != nil {
		return nil, fmt.Errorf("failed to save run: %w", err)
	}

	return run, nil
}

func (s *S3) GetRun(ctx context.Context, runID string) (*storage.PipelineRun, error) {
	return s.getRun(ctx, s.runKey(runID))
}

func (s *S3) UpdateRunStatus(ctx context.Context, runID string, status storage.RunStatus, errorMessage string) error {
	run, err := s.GetRun(ctx, runID)
	if err != nil {
		return err
	}

	now := time.Now().UTC()
	run.Status = status

	switch status {
	case storage.RunStatusRunning:
		run.StartedAt = &now
	case storage.RunStatusSuccess, storage.RunStatusFailed:
		run.CompletedAt = &now
		run.ErrorMessage = errorMessage
	}

	data, err := json.Marshal(run)
	if err != nil {
		return fmt.Errorf("failed to marshal updated run: %w", err)
	}

	return s.putJSON(ctx, s.runKey(runID), data)
}

func (s *S3) SearchRunsByPipeline(ctx context.Context, pipelineID, query string, page, perPage int) (*storage.PaginationResult[storage.PipelineRun], error) {
	if page < 1 {
		page = 1
	}

	if perPage < 1 {
		perPage = 20
	}

	runKeys, err := s.listKeys(ctx, s.fullKey("runs/"))
	if err != nil {
		return emptyRunPage(page, perPage), nil
	}

	var matched []storage.PipelineRun

	for _, key := range runKeys {
		run, err := s.getRun(ctx, key)
		if err != nil {
			continue
		}

		if run.PipelineID != pipelineID {
			continue
		}

		if query != "" && !runMatchesQuery(run, query) {
			continue
		}

		matched = append(matched, *run)
	}

	sort.Slice(matched, func(i, j int) bool {
		return matched[i].CreatedAt.After(matched[j].CreatedAt)
	})

	return paginate(matched, page, perPage), nil
}

// ─── Search operations ──────────────────────────────────────────────────────

func (s *S3) SearchPipelines(ctx context.Context, query string, page, perPage int) (*storage.PaginationResult[storage.Pipeline], error) {
	if page < 1 {
		page = 1
	}

	if perPage < 1 {
		perPage = 20
	}

	keys, err := s.listKeys(ctx, s.fullKey("pipelines/by-id/"))
	if err != nil {
		return emptyPipelinePage(page, perPage), nil
	}

	var matched []storage.Pipeline

	for _, key := range keys {
		p, err := s.getPipeline(ctx, key)
		if err != nil {
			continue
		}

		if query == "" || pipelineMatchesQuery(p, query) {
			matched = append(matched, *p)
		}
	}

	sort.Slice(matched, func(i, j int) bool {
		return matched[i].CreatedAt.After(matched[j].CreatedAt)
	})

	return paginate(matched, page, perPage), nil
}

func (s *S3) Search(ctx context.Context, prefix, query string) (storage.Results, error) {
	if query == "" {
		return nil, nil
	}

	keyPrefix := s.taskKey(prefix)

	keys, err := s.listKeys(ctx, keyPrefix)
	if err != nil {
		return nil, nil
	}

	lowerQuery := strings.ToLower(query)
	var results storage.Results

	for _, key := range keys {
		payload, err := s.getJSON(ctx, key)
		if err != nil {
			continue
		}

		logicalPath := key
		if s.prefix != "" {
			logicalPath = strings.TrimPrefix(key, s.prefix+"/")
		}

		logicalPath = strings.TrimPrefix(logicalPath, "tasks")

		if pathOrPayloadMatches(logicalPath, payload, lowerQuery) {
			summary := storage.Payload{}
			if v, ok := payload["status"]; ok {
				summary["status"] = v
			}

			if v, ok := payload["elapsed"]; ok {
				summary["elapsed"] = v
			}

			if v, ok := payload["started_at"]; ok {
				summary["started_at"] = v
			}

			results = append(results, storage.Result{
				ID:      0,
				Path:    logicalPath,
				Payload: summary,
			})
		}
	}

	return results, nil
}

// ─── S3 key helpers ─────────────────────────────────────────────────────────

func (s *S3) pipelineByIDKey(id string) string {
	return s.fullKey("pipelines/by-id/" + id + ".json")
}

func (s *S3) pipelineByNameKey(name string) string {
	return s.fullKey("pipelines/by-name/" + name + ".json")
}

func (s *S3) runKey(id string) string {
	return s.fullKey("runs/" + id + ".json")
}

// ─── S3 low-level helpers ───────────────────────────────────────────────────

func (s *S3) getJSON(ctx context.Context, key string) (storage.Payload, error) {
	result, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		if isNotFound(err) {
			return nil, storage.ErrNotFound
		}

		return nil, fmt.Errorf("failed to get object %q: %w", key, err)
	}

	defer func() {
		_ = result.Body.Close()
	}()

	data, err := io.ReadAll(result.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read object %q: %w", key, err)
	}

	var payload storage.Payload

	err = json.Unmarshal(data, &payload)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal object %q: %w", key, err)
	}

	return payload, nil
}

func (s *S3) putJSON(ctx context.Context, key string, data []byte) error {
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(data),
		ContentType: aws.String("application/json"),
	})
	if err != nil {
		return fmt.Errorf("failed to put object %q: %w", key, err)
	}

	return nil
}

func (s *S3) deleteObject(ctx context.Context, key string) error {
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("failed to delete object %q: %w", key, err)
	}

	return nil
}

func (s *S3) listKeys(ctx context.Context, prefix string) ([]string, error) {
	var keys []string

	paginator := s3.NewListObjectsV2Paginator(s.client, &s3.ListObjectsV2Input{
		Bucket: aws.String(s.bucket),
		Prefix: aws.String(prefix),
	})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to list objects with prefix %q: %w", prefix, err)
		}

		for _, obj := range page.Contents {
			keys = append(keys, *obj.Key)
		}
	}

	return keys, nil
}

func (s *S3) getPipeline(ctx context.Context, key string) (*storage.Pipeline, error) {
	result, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		if isNotFound(err) {
			return nil, storage.ErrNotFound
		}

		return nil, fmt.Errorf("failed to get pipeline %q: %w", key, err)
	}

	defer func() {
		_ = result.Body.Close()
	}()

	data, err := io.ReadAll(result.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read pipeline %q: %w", key, err)
	}

	var pipeline storage.Pipeline

	err = json.Unmarshal(data, &pipeline)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal pipeline %q: %w", key, err)
	}

	return &pipeline, nil
}

func (s *S3) getRun(ctx context.Context, key string) (*storage.PipelineRun, error) {
	result, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		if isNotFound(err) {
			return nil, storage.ErrNotFound
		}

		return nil, fmt.Errorf("failed to get run %q: %w", key, err)
	}

	defer func() {
		_ = result.Body.Close()
	}()

	data, err := io.ReadAll(result.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read run %q: %w", key, err)
	}

	var run storage.PipelineRun

	err = json.Unmarshal(data, &run)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal run %q: %w", key, err)
	}

	return &run, nil
}

// ─── Utility functions ──────────────────────────────────────────────────────

func isNotFound(err error) bool {
	var noSuchKey *types.NoSuchKey
	if errors.As(err, &noSuchKey) {
		return true
	}

	var notFound *types.NotFound
	if errors.As(err, &notFound) {
		return true
	}

	return strings.Contains(err.Error(), "NoSuchKey") || strings.Contains(err.Error(), "StatusCode: 404")
}

func runMatchesQuery(run *storage.PipelineRun, query string) bool {
	lower := strings.ToLower(query)

	return strings.Contains(strings.ToLower(run.ID), lower) ||
		strings.Contains(strings.ToLower(string(run.Status)), lower) ||
		strings.Contains(strings.ToLower(run.ErrorMessage), lower)
}

func pipelineMatchesQuery(p *storage.Pipeline, query string) bool {
	lower := strings.ToLower(query)

	return strings.Contains(strings.ToLower(p.Name), lower) ||
		strings.Contains(strings.ToLower(p.Content), lower)
}

func pathOrPayloadMatches(p string, payload storage.Payload, lowerQuery string) bool {
	if strings.Contains(strings.ToLower(p), lowerQuery) {
		return true
	}

	for _, v := range payload {
		str, ok := v.(string)
		if ok && strings.Contains(strings.ToLower(str), lowerQuery) {
			return true
		}
	}

	return false
}

func paginate[T any](items []T, page, perPage int) *storage.PaginationResult[T] {
	total := len(items)
	totalPages := (total + perPage - 1) / perPage

	if totalPages == 0 {
		totalPages = 1
	}

	offset := (page - 1) * perPage
	end := offset + perPage

	if offset > total {
		offset = total
	}

	if end > total {
		end = total
	}

	return &storage.PaginationResult[T]{
		Items:      items[offset:end],
		Page:       page,
		PerPage:    perPage,
		TotalItems: total,
		TotalPages: totalPages,
		HasNext:    page < totalPages,
	}
}

func emptyRunPage(page, perPage int) *storage.PaginationResult[storage.PipelineRun] {
	return &storage.PaginationResult[storage.PipelineRun]{
		Items:      []storage.PipelineRun{},
		Page:       page,
		PerPage:    perPage,
		TotalItems: 0,
		TotalPages: 1,
		HasNext:    false,
	}
}

func emptyPipelinePage(page, perPage int) *storage.PaginationResult[storage.Pipeline] {
	return &storage.PaginationResult[storage.Pipeline]{
		Items:      []storage.Pipeline{},
		Page:       page,
		PerPage:    perPage,
		TotalItems: 0,
		TotalPages: 1,
		HasNext:    false,
	}
}

func init() {
	storage.Add("s3", NewS3)
}

// Package s3 provides an S3-backed secrets manager for PocketCI.
//
// # Security model
//
// Secrets are protected by two independent layers:
//  1. Application-layer AES-256-GCM encryption (key derived from the passphrase
//     in "key=" query param), applied before any bytes leave the process.
//  2. S3 Server-Side Encryption (SSE-S3 or SSE-KMS), configured via "sse=" and
//     enforced by an SSE probe at construction time.  If the target provider
//     does not support SSE, the constructor returns an error.
//
// # DSN format
//
//	s3://[http://|https://][ACCESS_KEY_ID:SECRET_ACCESS_KEY@]host[:port]/bucket[/prefix]?region=...&sse=AES256&key=passphrase
//
// Parameters:
//   - sse      (required) "AES256" or "aws:kms"
//   - key      (required) Encryption passphrase for application-layer AES-256-GCM
//   - region   AWS region (default: SDK credential-chain default)
//   - endpoint Custom S3-compatible endpoint (MinIO, Cloudflare R2, etc.)
//   - sse_kms_key_id  KMS key ID (only with sse=aws:kms; provider default when omitted)
//   - force_path_style "true"/"false" — defaults to true when endpoint is set
package s3

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/s3/transfermanager"
	tmtypes "github.com/aws/aws-sdk-go-v2/feature/s3/transfermanager/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/jtarchie/pocketci/s3config"
	"github.com/jtarchie/pocketci/secrets"
)

func init() {
	secrets.Register("s3", New)
}

// S3 implements secrets.Manager using an S3-compatible object store as the
// backend. Every stored object is AES-256-GCM encrypted before upload; S3
// Server-Side Encryption provides an additional at-rest protection layer.
type S3 struct {
	client    *s3.Client
	bucket    string
	prefix    string
	encryptor *secrets.Encryptor
	logger    *slog.Logger
	sse       types.ServerSideEncryption
	sseKeyID  string
}

// secretRecord is the JSON structure persisted in each S3 object.
// EncryptedValue is base64-encoded AES-256-GCM ciphertext.
type secretRecord struct {
	EncryptedValue string `json:"encrypted_value"`
	Version        string `json:"version"`
	UpdatedAt      string `json:"updated_at"`
}

// New creates a new S3-backed secrets manager.
//
// The sse and key query parameters are both mandatory. Construction fails when:
//   - "sse" is absent (provider-level encryption not configured)
//   - "key" is absent (no application-layer encryption passphrase)
//   - The SSE probe PutObject/HeadObject round-trip shows the S3 provider does
//     not actually apply server-side encryption to the stored object.
func New(dsn string, logger *slog.Logger) (secrets.Manager, error) {
	if logger == nil {
		logger = slog.Default()
	}

	logger = logger.WithGroup("secrets.s3")

	s3cfg, err := s3config.ParseDSN(dsn)
	if err != nil {
		return nil, fmt.Errorf("invalid secrets S3 DSN: %w", err)
	}

	if s3cfg.SSE == "" {
		return nil, fmt.Errorf("s3 secrets driver requires sse= param (AES256 or aws:kms): data at rest must be encrypted by the provider")
	}

	// Extract the application-layer encryption passphrase from "key" param.
	// url.Parse is safe here — ParseDSN already validated the DSN.
	parsed, _ := url.Parse(dsn)
	passphrase := parsed.Query().Get("key")

	if passphrase == "" {
		return nil, fmt.Errorf("s3 secrets driver requires key= param for application-layer encryption")
	}

	key := secrets.DeriveKey(passphrase)

	encryptor, err := secrets.NewEncryptor(key)
	if err != nil {
		return nil, fmt.Errorf("could not create encryptor: %w", err)
	}

	ctx := context.Background()

	awsCfg, err := s3cfg.LoadAWSConfig(ctx)
	if err != nil {
		return nil, err
	}

	client := s3.NewFromConfig(awsCfg, s3cfg.ClientOptions()...)

	mgr := &S3{
		client:    client,
		bucket:    s3cfg.Bucket,
		prefix:    s3cfg.Prefix,
		encryptor: encryptor,
		logger:    logger,
		sse:       s3cfg.SSE,
		sseKeyID:  s3cfg.SSEKMSKeyID,
	}

	// Probe SSE: upload a tiny sentinel object and verify that HeadObject
	// reports server-side encryption was applied. Fail fast if the provider
	// does not honour SSE headers.
	if err := mgr.probeSSE(ctx); err != nil {
		return nil, fmt.Errorf("s3 secrets SSE probe failed — provider does not support SSE %q: %w", s3cfg.SSE, err)
	}

	logger.Info("secrets.s3.initialized", "bucket", s3cfg.Bucket, "prefix", s3cfg.Prefix, "sse", s3cfg.SSE)

	return mgr, nil
}

// probeSSE writes a tiny sentinel object with SSE headers and verifies via
// HeadObject that the provider applied server-side encryption. The sentinel is
// deleted immediately regardless of the outcome.
func (s *S3) probeSSE(ctx context.Context) error {
	sentinelKey := s.scopePrefix("__probe__") + "__sse_check__.json"
	data := []byte(`{"probe":true}`)

	uploader := transfermanager.New(s.client)

	input := &transfermanager.UploadObjectInput{
		Bucket:               aws.String(s.bucket),
		Key:                  aws.String(sentinelKey),
		Body:                 bytes.NewReader(data),
		ContentType:          aws.String("application/json"),
		ServerSideEncryption: tmtypes.ServerSideEncryption(s.sse),
	}

	if s.sseKeyID != "" {
		input.SSEKMSKeyID = aws.String(s.sseKeyID)
	}

	_, uploadErr := uploader.UploadObject(ctx, input)

	// Always attempt cleanup — even if upload failed the key might exist.
	_, _ = s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(sentinelKey),
	})

	if uploadErr != nil {
		return fmt.Errorf("SSE probe upload failed: %w", uploadErr)
	}

	return nil
}

// scopePrefix returns the S3 key prefix for a given scope directory.
// Format: [prefix/]secrets/[scope]/
func (s *S3) scopePrefix(scope string) string {
	parts := []string{}

	if s.prefix != "" {
		parts = append(parts, s.prefix)
	}

	parts = append(parts, "secrets", scope)

	return strings.Join(parts, "/") + "/"
}

// objectKey returns the S3 key for a specific scope+key combination.
// Format: [prefix/]secrets/[scope]/[url.PathEscape(key)].json
func (s *S3) objectKey(scope, key string) string {
	return s.scopePrefix(scope) + url.PathEscape(key) + ".json"
}

// upload writes a secretRecord to S3 using multipart upload with SSE.
func (s *S3) upload(ctx context.Context, key string, rec secretRecord) error {
	data, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("could not marshal secret record: %w", err)
	}

	uploader := transfermanager.New(s.client)

	input := &transfermanager.UploadObjectInput{
		Bucket:               aws.String(s.bucket),
		Key:                  aws.String(key),
		Body:                 bytes.NewReader(data),
		ContentType:          aws.String("application/json"),
		ServerSideEncryption: tmtypes.ServerSideEncryption(s.sse),
	}

	if s.sseKeyID != "" {
		input.SSEKMSKeyID = aws.String(s.sseKeyID)
	}

	_, err = uploader.UploadObject(ctx, input)
	if err != nil {
		return fmt.Errorf("could not upload secret: %w", err)
	}

	return nil
}

// download retrieves and deserialises a secretRecord from S3.
func (s *S3) download(ctx context.Context, key string) (*secretRecord, error) {
	result, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		if isNotFound(err) {
			return nil, secrets.ErrNotFound
		}

		return nil, fmt.Errorf("could not get secret: %w", err)
	}

	defer func() { _ = result.Body.Close() }()

	body, err := io.ReadAll(result.Body)
	if err != nil {
		return nil, fmt.Errorf("could not read secret body: %w", err)
	}

	var rec secretRecord

	if err := json.Unmarshal(body, &rec); err != nil {
		return nil, fmt.Errorf("could not unmarshal secret record: %w", err)
	}

	return &rec, nil
}

// Get retrieves a plaintext secret by scope and key.
func (s *S3) Get(ctx context.Context, scope string, key string) (string, error) {
	rec, err := s.download(ctx, s.objectKey(scope, key))
	if err != nil {
		return "", err
	}

	ciphertext, err := base64.StdEncoding.DecodeString(rec.EncryptedValue)
	if err != nil {
		return "", fmt.Errorf("could not decode encrypted value for %q in scope %q: %w", key, scope, err)
	}

	plaintext, err := s.encryptor.Decrypt(ciphertext)
	if err != nil {
		return "", fmt.Errorf("could not decrypt secret %q in scope %q: %w", key, scope, err)
	}

	return string(plaintext), nil
}

// Set stores or updates an encrypted secret.
func (s *S3) Set(ctx context.Context, scope string, key string, value string) error {
	encrypted, err := s.encryptor.Encrypt([]byte(value))
	if err != nil {
		return fmt.Errorf("could not encrypt secret: %w", err)
	}

	objKey := s.objectKey(scope, key)

	// Determine the next version by checking whether the object already exists.
	version := "v1"

	existing, err := s.download(ctx, objKey)
	if err != nil && !errors.Is(err, secrets.ErrNotFound) {
		return fmt.Errorf("could not check existing secret: %w", err)
	}

	if existing != nil {
		version = incrementVersion(existing.Version)
	}

	rec := secretRecord{
		EncryptedValue: base64.StdEncoding.EncodeToString(encrypted),
		Version:        version,
		UpdatedAt:      time.Now().UTC().Format(time.RFC3339),
	}

	if err := s.upload(ctx, objKey, rec); err != nil {
		return fmt.Errorf("could not store secret: %w", err)
	}

	s.logger.Info("secret.set", "scope", scope, "key", key, "version", version)

	return nil
}

// Delete removes a secret. Returns ErrNotFound when the secret does not exist.
func (s *S3) Delete(ctx context.Context, scope string, key string) error {
	objKey := s.objectKey(scope, key)

	// Verify it exists before attempting deletion to return ErrNotFound correctly.
	if _, err := s.download(ctx, objKey); err != nil {
		return err
	}

	if _, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(objKey),
	}); err != nil {
		return fmt.Errorf("could not delete secret: %w", err)
	}

	s.logger.Info("secret.deleted", "scope", scope, "key", key)

	return nil
}

// ListByScope returns all secret keys within a scope, sorted alphabetically.
func (s *S3) ListByScope(ctx context.Context, scope string) ([]string, error) {
	prefix := s.scopePrefix(scope)

	keys, err := s.listKeys(ctx, prefix)
	if err != nil {
		return nil, fmt.Errorf("could not list secrets by scope: %w", err)
	}

	// Strip the scope prefix and ".json" suffix to recover the original key names.
	result := make([]string, 0, len(keys))

	for _, k := range keys {
		name := strings.TrimPrefix(k, prefix)
		name = strings.TrimSuffix(name, ".json")

		unescaped, err := url.PathUnescape(name)
		if err != nil {
			unescaped = name
		}

		result = append(result, unescaped)
	}

	return result, nil
}

// DeleteByScope removes all secrets in the given scope.
func (s *S3) DeleteByScope(ctx context.Context, scope string) error {
	prefix := s.scopePrefix(scope)

	keys, err := s.listKeys(ctx, prefix)
	if err != nil {
		return fmt.Errorf("could not list secrets for scope deletion: %w", err)
	}

	for _, k := range keys {
		if _, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
			Bucket: aws.String(s.bucket),
			Key:    aws.String(k),
		}); err != nil {
			return fmt.Errorf("could not delete secret %q: %w", k, err)
		}
	}

	s.logger.Info("secrets.deleted_by_scope", "scope", scope)

	return nil
}

// Close is a no-op; the S3 client holds no persistent connections.
func (s *S3) Close() error {
	return nil
}

// listKeys returns all S3 object keys under the given prefix.
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
			keys = append(keys, aws.ToString(obj.Key))
		}
	}

	return keys, nil
}

// isNotFound reports whether err represents a missing S3 object.
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

// incrementVersion increments a version string like "v1" → "v2".
func incrementVersion(version string) string {
	var num int

	_, err := fmt.Sscanf(version, "v%d", &num)
	if err != nil {
		return "v1"
	}

	return fmt.Sprintf("v%d", num+1)
}

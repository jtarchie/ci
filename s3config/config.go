// Package s3config provides shared S3 DSN parsing and client option building
// used by all PocketCI S3 drivers (storage backend, volume cache).
//
// DSN format:
//
//	s3://bucket/optional/prefix?region=us-east-1&endpoint=http://localhost:9000
package s3config

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// Config holds the parsed configuration from an S3 DSN.
type Config struct {
	Bucket string
	Prefix string

	// Region is the AWS region. Uses the SDK credential-chain default when empty.
	Region string

	// Endpoint is the custom S3-compatible base URL (MinIO, Cloudflare R2, etc.).
	Endpoint string

	// ForcePathStyle enables path-style S3 URLs (required for most non-AWS
	// endpoints). Defaults to true whenever Endpoint is set; can be overridden
	// with force_path_style=false when virtual-hosted-style is required (e.g.
	// Cloudflare R2 with custom domain).
	ForcePathStyle bool

	// SSE is the server-side encryption algorithm.
	// Supported values: "" (none, default), "AES256", "aws:kms".
	SSE types.ServerSideEncryption

	// SSEKMSKeyID is the KMS key ID used when SSE == "aws:kms".
	// When empty the provider's default KMS key is used.
	SSEKMSKeyID string

	// TTL is the optional cache expiry duration.
	// Only populated when the ttl query parameter is present.
	// Storage drivers ignore this field.
	TTL time.Duration
}

// ParseDSN parses an S3 DSN and returns a validated Config.
//
// Supported query parameters:
//   - region            AWS region (default: SDK credential-chain default)
//   - endpoint          Custom S3-compatible endpoint URL (MinIO, R2, etc.)
//   - force_path_style  "true" | "false" — defaults to true when endpoint is set
//   - sse               "AES256" | "aws:kms" — server-side encryption; omit for none
//   - sse_kms_key_id    KMS key ID (only with sse=aws:kms; provider default when omitted)
//   - ttl               Cache expiry duration, e.g. "24h" (cache driver only)
func ParseDSN(dsn string) (*Config, error) {
	parsed, err := url.Parse(dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to parse S3 DSN %q: %w", dsn, err)
	}

	if parsed.Scheme != "s3" {
		return nil, fmt.Errorf("expected s3:// DSN, got %s://", parsed.Scheme)
	}

	if parsed.Host == "" {
		return nil, fmt.Errorf("S3 DSN missing bucket name in %q", dsn)
	}

	cfg := &Config{
		Bucket: parsed.Host,
		Prefix: strings.TrimPrefix(parsed.Path, "/"),
	}

	q := parsed.Query()

	cfg.Region = q.Get("region")
	cfg.Endpoint = q.Get("endpoint")

	// Path-style defaults to true when a custom endpoint is set — required for
	// MinIO, DigitalOcean Spaces, and most non-AWS S3-compatible stores.
	// Callers can opt out via force_path_style=false for providers that require
	// virtual-hosted-style URLs (e.g. some Cloudflare R2 configurations).
	if q.Get("force_path_style") == "false" {
		cfg.ForcePathStyle = false
	} else {
		cfg.ForcePathStyle = cfg.Endpoint != ""
	}

	switch sse := q.Get("sse"); sse {
	case "":
		// No SSE configured — preserve current default: no explicit SSE headers.
	case "AES256":
		cfg.SSE = types.ServerSideEncryptionAes256
	case "aws:kms":
		cfg.SSE = types.ServerSideEncryptionAwsKms
	default:
		return nil, fmt.Errorf("unsupported sse value %q: must be AES256 or aws:kms", sse)
	}

	cfg.SSEKMSKeyID = q.Get("sse_kms_key_id")

	if ttlStr := q.Get("ttl"); ttlStr != "" {
		ttl, err := time.ParseDuration(ttlStr)
		if err != nil {
			return nil, fmt.Errorf("failed to parse ttl %q: %w", ttlStr, err)
		}

		cfg.TTL = ttl
	}

	return cfg, nil
}

// ClientOptions returns the S3 client functional options derived from Region
// and Endpoint. Pass the result as variadic args to s3.NewFromConfig().
func (c *Config) ClientOptions() []func(*s3.Options) {
	var opts []func(*s3.Options)

	if c.Region != "" {
		region := c.Region
		opts = append(opts, func(o *s3.Options) {
			o.Region = region
		})
	}

	if c.Endpoint != "" {
		endpoint := c.Endpoint
		forcePathStyle := c.ForcePathStyle
		opts = append(opts, func(o *s3.Options) {
			o.BaseEndpoint = aws.String(endpoint)
			o.UsePathStyle = forcePathStyle
		})
	}

	return opts
}

// ApplySSEToPut sets server-side encryption fields on a PutObjectInput when
// SSE is configured. No-op when SSE is not set (preserves current default).
func (c *Config) ApplySSEToPut(input *s3.PutObjectInput) {
	if c.SSE == "" {
		return
	}

	input.ServerSideEncryption = c.SSE
	if c.SSEKMSKeyID != "" {
		input.SSEKMSKeyId = aws.String(c.SSEKMSKeyID)
	}
}

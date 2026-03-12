// Package s3config provides shared S3 DSN parsing and client option building
// used by all PocketCI S3 drivers (storage backend, volume cache).
//
// DSN format:
//
//	s3://[http://|https://][ACCESS_KEY_ID:SECRET_ACCESS_KEY@]host[:port]/bucket[/prefix]?region=...&encrypt=...
//
// When no http/https scheme prefix is provided, https is assumed. When a custom
// host is omitted entirely (bare bucket-only form) the AWS SDK default endpoint
// is used.
//
// Examples:
//
//	s3://s3.amazonaws.com/mybucket/prefix?region=us-east-1
//	s3://http://localhost:9000/mybucket?region=us-east-1&encrypt=sse-s3
//	s3://http://minioadmin:minioadmin@localhost:9000/mybucket?region=us-east-1
//	s3://https://AKID:SECRET@account.r2.cloudflarestorage.com/mybucket?region=auto
package s3config

import (
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/s3/transfermanager"
	tmtypes "github.com/aws/aws-sdk-go-v2/feature/s3/transfermanager/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// Config holds the parsed configuration from an S3 DSN.
type Config struct {
	Bucket string
	Prefix string

	// AccessKeyID and SecretAccessKey are static credentials parsed from the
	// DSN userinfo (s3://http://ID:SECRET@host/bucket/...). When empty the SDK
	// credential chain (env vars, ~/.aws/credentials, IAM role, etc.) is used.
	AccessKeyID     string
	SecretAccessKey string

	// Region is the AWS region. Uses the SDK credential-chain default when empty.
	Region string

	// Endpoint is the custom S3-compatible base URL (MinIO, Cloudflare R2, etc.).
	Endpoint string

	// ForcePathStyle enables path-style S3 URLs (required for most non-AWS
	// endpoints). Defaults to true whenever Endpoint is set; can be overridden
	// with force_path_style=false when virtual-hosted-style is required (e.g.
	// Cloudflare R2 with custom domain).
	ForcePathStyle bool

	// EncryptMode is the server-side encryption mode.
	// Values: "" (none, default), "sse-s3" (AES-256), "sse-kms" (KMS), "sse-c" (customer-provided key).
	EncryptMode string

	// SSEKMSKeyID is the KMS key ID used when EncryptMode == "sse-kms".
	// When empty the provider's default KMS key is used.
	SSEKMSKeyID string

	// SSECKey is the 32-byte customer-provided key used when EncryptMode == "sse-c".
	// Derived as SHA-256 of the key= passphrase.
	SSECKey []byte

	// Key is the raw passphrase from the key= query parameter.
	// Used by the secrets driver for application-layer AES-256-GCM encryption,
	// and as the source material for SSECKey when EncryptMode == "sse-c".
	Key string

	// TTL is the optional cache expiry duration.
	// Only populated when the ttl query parameter is present.
	// Storage drivers ignore this field.
	TTL time.Duration
}

// ParseDSN parses an S3 DSN and returns a validated Config.
//
// Format:
//
//	s3://[http://|https://][ACCESS_KEY_ID:SECRET_ACCESS_KEY@]host[:port]/bucket[/prefix]?params
//
// When no http/https scheme prefix is present, https is assumed. Credentials
// are optional; when absent the AWS SDK credential chain is used (env vars,
// ~/.aws/credentials, IAM role, etc.).
//
// Supported query parameters:
//   - region            AWS region (default: SDK credential-chain default)
//   - force_path_style  "true" | "false" — defaults to true when a custom host is set
//   - encrypt           "sse-s3" | "sse-kms" | "sse-c" — server-side encryption; omit for none
//   - sse_kms_key_id    KMS key ID (only with encrypt=sse-kms; provider default when omitted)
//   - key               Passphrase — required when encrypt=sse-c; also used by the secrets
//     driver for application-layer AES-256-GCM encryption
//   - ttl               Cache expiry duration, e.g. "24h" (cache driver only)
func ParseDSN(dsn string) (*Config, error) {
	const prefix = "s3://"

	if !strings.HasPrefix(dsn, prefix) {
		return nil, fmt.Errorf("expected s3:// DSN, got %q", dsn)
	}

	// Strip the "s3://" scheme to get the inner URI, then normalise to an
	// http/https URL so url.Parse can extract host, credentials, and path.
	inner := dsn[len(prefix):]

	if inner == "" {
		return nil, fmt.Errorf("S3 DSN missing host/bucket in %q", dsn)
	}

	var innerURL string

	if strings.HasPrefix(inner, "http://") || strings.HasPrefix(inner, "https://") {
		innerURL = inner
	} else {
		// No explicit scheme — assume https (covers bare AWS S3 hostnames and
		// the common s3://host/bucket shorthand).
		innerURL = "https://" + inner
	}

	parsed, err := url.Parse(innerURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse S3 DSN %q: %w", dsn, err)
	}

	// Extract bucket (first path segment) and optional prefix (remainder).
	pathParts := strings.SplitN(strings.TrimPrefix(parsed.Path, "/"), "/", 2)
	bucket := pathParts[0]

	if bucket == "" {
		return nil, fmt.Errorf("S3 DSN missing bucket name in %q", dsn)
	}

	cfg := &Config{
		Bucket: bucket,
	}

	if len(pathParts) == 2 {
		cfg.Prefix = pathParts[1]
	}

	// Endpoint: scheme + host (empty when using bare AWS hostname via SDK default).
	// We always set it when a host is present so callers can point at MinIO etc.
	cfg.Endpoint = parsed.Scheme + "://" + parsed.Host

	// Extract inline credentials from userinfo (http://ID:SECRET@host/...).
	if parsed.User != nil {
		cfg.AccessKeyID = parsed.User.Username()
		cfg.SecretAccessKey, _ = parsed.User.Password()
	}

	q := parsed.Query()

	cfg.Region = q.Get("region")

	// Path-style defaults to true when a custom endpoint is set — required for
	// MinIO, DigitalOcean Spaces, and most non-AWS S3-compatible stores.
	// Callers can opt out via force_path_style=false for providers that require
	// virtual-hosted-style URLs (e.g. Cloudflare R2 with custom domain).
	if q.Get("force_path_style") == "false" {
		cfg.ForcePathStyle = false
	} else {
		cfg.ForcePathStyle = cfg.Endpoint != ""
	}

	cfg.Key = q.Get("key")

	switch encrypt := q.Get("encrypt"); encrypt {
	case "":
		// No provider-level encryption.
	case "sse-s3":
		cfg.EncryptMode = "sse-s3"
	case "sse-kms":
		cfg.EncryptMode = "sse-kms"
	case "sse-c":
		if cfg.Key == "" {
			return nil, fmt.Errorf("encrypt=sse-c requires key= passphrase in DSN")
		}

		sum := sha256.Sum256([]byte(cfg.Key))
		cfg.SSECKey = sum[:]
		cfg.EncryptMode = "sse-c"
	default:
		return nil, fmt.Errorf("unsupported encrypt value %q: must be sse-s3, sse-kms, or sse-c", encrypt)
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

// LoadAWSConfig loads an AWS config using the SDK default credential chain,
// optionally overriding with static credentials when they were embedded in the DSN.
func (c *Config) LoadAWSConfig(ctx context.Context) (aws.Config, error) {
	var opts []func(*config.LoadOptions) error

	if c.AccessKeyID != "" && c.SecretAccessKey != "" {
		opts = append(opts, config.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(c.AccessKeyID, c.SecretAccessKey, ""),
		))
	}

	awsCfg, err := config.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return aws.Config{}, fmt.Errorf("failed to load AWS config: %w", err)
	}

	return awsCfg, nil
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

// ssecStrings returns the base64-encoded key and key MD5 for SSE-C operations.
// Only valid when EncryptMode == "sse-c".
func (c *Config) ssecStrings() (algorithm, key, keyMD5 string) {
	b64key := base64.StdEncoding.EncodeToString(c.SSECKey)
	sum := md5.Sum(c.SSECKey) //nolint:gosec // SSE-C protocol requires MD5 of the key, not for security
	b64md5 := base64.StdEncoding.EncodeToString(sum[:])

	return "AES256", b64key, b64md5
}

// ApplySSEToPut sets server-side encryption fields on a PutObjectInput.
// No-op when EncryptMode is empty.
func (c *Config) ApplySSEToPut(input *s3.PutObjectInput) {
	switch c.EncryptMode {
	case "sse-s3":
		input.ServerSideEncryption = types.ServerSideEncryptionAes256
	case "sse-kms":
		input.ServerSideEncryption = types.ServerSideEncryptionAwsKms
		if c.SSEKMSKeyID != "" {
			input.SSEKMSKeyId = aws.String(c.SSEKMSKeyID)
		}
	case "sse-c":
		algorithm, key, keyMD5 := c.ssecStrings()
		input.SSECustomerAlgorithm = aws.String(algorithm)
		input.SSECustomerKey = aws.String(key)
		input.SSECustomerKeyMD5 = aws.String(keyMD5)
	}
}

// ApplySSEToGet sets SSE-C fields on a GetObjectInput.
// No-op when EncryptMode is not "sse-c" (server-managed modes need no headers on reads).
func (c *Config) ApplySSEToGet(input *s3.GetObjectInput) {
	if c.EncryptMode != "sse-c" {
		return
	}

	algorithm, key, keyMD5 := c.ssecStrings()
	input.SSECustomerAlgorithm = aws.String(algorithm)
	input.SSECustomerKey = aws.String(key)
	input.SSECustomerKeyMD5 = aws.String(keyMD5)
}

// ApplySSEToHead sets SSE-C fields on a HeadObjectInput.
// No-op when EncryptMode is not "sse-c".
func (c *Config) ApplySSEToHead(input *s3.HeadObjectInput) {
	if c.EncryptMode != "sse-c" {
		return
	}

	algorithm, key, keyMD5 := c.ssecStrings()
	input.SSECustomerAlgorithm = aws.String(algorithm)
	input.SSECustomerKey = aws.String(key)
	input.SSECustomerKeyMD5 = aws.String(keyMD5)
}

// ApplySSEToUpload sets server-side encryption fields on a transfermanager UploadObjectInput.
// No-op when EncryptMode is empty.
func (c *Config) ApplySSEToUpload(input *transfermanager.UploadObjectInput) {
	switch c.EncryptMode {
	case "sse-s3":
		input.ServerSideEncryption = tmtypes.ServerSideEncryptionAes256
	case "sse-kms":
		input.ServerSideEncryption = tmtypes.ServerSideEncryptionAwsKms
		if c.SSEKMSKeyID != "" {
			input.SSEKMSKeyID = aws.String(c.SSEKMSKeyID)
		}
	case "sse-c":
		algorithm, key, keyMD5 := c.ssecStrings()
		input.SSECustomerAlgorithm = aws.String(algorithm)
		input.SSECustomerKey = aws.String(key)
		input.SSECustomerKeyMD5 = aws.String(keyMD5)
	}
}

package s3

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/aws/aws-sdk-go-v2/feature/s3/transfermanager"
	"github.com/jtarchie/pocketci/orchestra/cache"
	"github.com/jtarchie/pocketci/s3config"
)

func init() {
	cache.RegisterCacheStore("s3", NewS3Store)
}

// S3Store implements CacheStore using AWS S3.
type S3Store struct {
	*s3config.Client
	ttl time.Duration
}

// Option configures an S3Store.
type Option func(*S3Store)

// WithTTL sets the TTL for cached objects.
func WithTTL(ttl time.Duration) Option {
	return func(s *S3Store) {
		s.ttl = ttl
	}
}

// NewS3Store creates a new S3-backed cache store.
// URL format: s3://http://localhost:9000/bucket/prefix?region=us-east-1
func NewS3Store(urlStr string) (cache.CacheStore, error) {
	s3cfg, err := s3config.ParseDSN(urlStr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse S3 URL: %w", err)
	}

	client, err := s3config.NewClient(context.Background(), s3cfg)
	if err != nil {
		return nil, err
	}

	return &S3Store{
		Client: client,
		ttl:    s3cfg.TTL,
	}, nil
}

// Restore downloads cached content from S3.
func (s *S3Store) Restore(ctx context.Context, key string) (io.ReadCloser, error) {
	fullKey := s.FullKey(key)

	result, err := s.GetStream(ctx, fullKey)
	if err != nil {
		if s3config.IsNotFound(err) {
			return nil, nil // Cache miss - not an error
		}

		return nil, fmt.Errorf("failed to get object from S3: %w", err)
	}

	// Check if object has expired based on TTL
	if s.ttl > 0 && result.LastModified != nil {
		if time.Since(*result.LastModified) > s.ttl {
			_ = result.Body.Close()
			// Object expired, delete it and return cache miss
			_ = s.Delete(ctx, key)

			return nil, nil
		}
	}

	return result.Body, nil
}

// Persist uploads content to S3 using streaming multipart upload.
// Data is uploaded in chunks without buffering the entire content in memory.
func (s *S3Store) Persist(ctx context.Context, key string, reader io.Reader) error {
	fullKey := s.FullKey(key)

	err := s.PutStream(ctx, fullKey, reader, func(u *transfermanager.Options) {
		// Use 10MB part size for efficient streaming
		u.PartSizeBytes = 10 * 1024 * 1024
		// Upload 3 parts concurrently
		u.Concurrency = 3
	})
	if err != nil {
		return fmt.Errorf("failed to upload to S3: %w", err)
	}

	return nil
}

// Exists checks if a cache key exists in S3.
func (s *S3Store) Exists(ctx context.Context, key string) (bool, error) {
	fullKey := s.FullKey(key)

	result, err := s.HeadKey(ctx, fullKey)
	if err != nil {
		if s3config.IsNotFound(err) {
			return false, nil
		}

		return false, fmt.Errorf("failed to check object existence: %w", err)
	}

	// Check TTL expiration
	if s.ttl > 0 && result.LastModified != nil {
		if time.Since(*result.LastModified) > s.ttl {
			return false, nil
		}
	}

	return true, nil
}

// Delete removes a cache entry from S3.
func (s *S3Store) Delete(ctx context.Context, key string) error {
	fullKey := s.FullKey(key)

	if err := s.DeleteKey(ctx, fullKey); err != nil {
		return fmt.Errorf("failed to delete object from S3: %w", err)
	}

	return nil
}

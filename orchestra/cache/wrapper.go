package cache

import (
	"fmt"
	"log/slog"
	"net/url"
	"strings"

	"github.com/jtarchie/ci/orchestra"
)

// WrapWithCaching wraps a driver with caching if cache parameters are present.
// Cache is configured via DSN parameters:
//   - cache: Cache store URL (e.g., "s3://bucket/prefix?region=us-east-1")
//   - cache_compression: Compression algorithm ("zstd", "gzip", "none"), defaults to "zstd"
//   - cache_prefix: Key prefix for cache entries, defaults to ""
//
// Example DSN: docker://namespace?cache=s3://bucket/prefix&cache_compression=zstd
//
// If cache parameter is not present, returns the original driver unchanged.
func WrapWithCaching(
	driver orchestra.Driver,
	params map[string]string,
	logger *slog.Logger,
) (orchestra.Driver, error) {
	cacheURL := params["cache"]
	if cacheURL == "" {
		return driver, nil
	}

	// URL-decode the cache URL in case it was encoded to avoid DSN parsing issues
	decodedURL, err := url.QueryUnescape(cacheURL)
	if err == nil {
		cacheURL = decodedURL
	}

	logger.Info("initializing cache layer",
		"cache_url", cacheURL,
		"driver", driver.Name(),
	)

	// Parse cache URL scheme
	parsed, err := url.Parse(cacheURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse cache URL: %w", err)
	}

	scheme := strings.ToLower(parsed.Scheme)

	// Get the cache store factory
	factory, ok := GetCacheStore(scheme)
	if !ok {
		return nil, fmt.Errorf("unknown cache store scheme: %s", scheme)
	}

	// Create the cache store
	store, err := factory(cacheURL)
	if err != nil {
		return nil, fmt.Errorf("failed to create cache store: %w", err)
	}

	// Get compression algorithm (default to zstd)
	compression := params["cache_compression"]
	if compression == "" {
		compression = "zstd"
	}

	compressor := NewCompressor(compression)

	// Get key prefix
	keyPrefix := params["cache_prefix"]

	// Check if driver supports volume data access
	if _, ok := driver.(VolumeDataAccessor); !ok {
		logger.Warn("driver does not support volume data access, caching disabled",
			"driver", driver.Name(),
		)

		return driver, nil
	}

	return NewCachingDriver(driver, store, compressor, keyPrefix, logger), nil
}

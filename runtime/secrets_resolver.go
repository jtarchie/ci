package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jtarchie/ci/secrets"
)

const secretPrefix = "secret:"

// resolveSecretString resolves a single "secret:<KEY>" value using the given
// secrets manager. Returns the resolved plaintext (true) when the value used
// the prefix, or the original value unchanged (false) when it did not.
func resolveSecretString(ctx context.Context, mgr secrets.Manager, pipelineID, value string) (string, bool, error) {
	if mgr == nil || !strings.HasPrefix(value, secretPrefix) {
		return value, false, nil
	}

	key := strings.TrimPrefix(value, secretPrefix)
	pipelineScope := secrets.PipelineScope(pipelineID)

	// Try pipeline scope first.
	val, err := mgr.Get(ctx, pipelineScope, key)
	if err == nil {
		return val, true, nil
	}

	if !errors.Is(err, secrets.ErrNotFound) {
		return "", false, fmt.Errorf("could not retrieve secret %q from scope %q: %w", key, pipelineScope, err)
	}

	// Fall back to global scope.
	val, err = mgr.Get(ctx, secrets.GlobalScope, key)
	if err == nil {
		return val, true, nil
	}

	if !errors.Is(err, secrets.ErrNotFound) {
		return "", false, fmt.Errorf("could not retrieve secret %q from scope %q: %w", key, secrets.GlobalScope, err)
	}

	return "", false, fmt.Errorf("secret %q not found in scopes %q or %q: %w",
		key, pipelineScope, secrets.GlobalScope, secrets.ErrNotFound)
}

// resolveSecretsInMap walks a map[string]any recursively and resolves any
// string value prefixed with "secret:<KEY>" using the given secrets manager.
// Each resolved plaintext secret is appended to *resolved (for redaction
// tracking); resolved may be nil if tracking is not needed.
func resolveSecretsInMap(ctx context.Context, mgr secrets.Manager, pipelineID string, m map[string]any, resolved *[]string) error {
	if mgr == nil || m == nil {
		return nil
	}

	for k, v := range m {
		switch val := v.(type) {
		case string:
			resolvedVal, wasSecret, err := resolveSecretString(ctx, mgr, pipelineID, val)
			if err != nil {
				return fmt.Errorf("key %q: %w", k, err)
			}

			if wasSecret {
				m[k] = resolvedVal

				if resolved != nil {
					*resolved = append(*resolved, resolvedVal)
				}
			}
		case map[string]any:
			if err := resolveSecretsInMap(ctx, mgr, pipelineID, val, resolved); err != nil {
				return fmt.Errorf("key %q: %w", k, err)
			}
		}
	}

	return nil
}

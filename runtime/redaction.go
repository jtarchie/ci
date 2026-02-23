package runtime

import (
	"sort"
	"strings"
)

const redactedPlaceholder = "***REDACTED***"

// RedactSecrets replaces all occurrences of secret values in text with ***REDACTED***.
// Uses longest-match-first ordering to avoid partial replacements.
func RedactSecrets(text string, secretValues []string) string {
	if len(secretValues) == 0 || text == "" {
		return text
	}

	// Filter out empty values and deduplicate
	seen := make(map[string]bool, len(secretValues))

	var values []string

	for _, v := range secretValues {
		if v != "" && !seen[v] {
			seen[v] = true
			values = append(values, v)
		}
	}

	if len(values) == 0 {
		return text
	}

	// Sort by length descending so longer values are replaced first
	// This prevents partial replacement of substrings
	sort.Slice(values, func(i, j int) bool {
		return len(values[i]) > len(values[j])
	})

	for _, secret := range values {
		text = strings.ReplaceAll(text, secret, redactedPlaceholder)
	}

	return text
}

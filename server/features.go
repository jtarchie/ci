package server

import (
	"fmt"
	"strings"
)

// Feature represents a gatable pipeline capability.
type Feature string

const (
	FeatureWebhooks      Feature = "webhooks"
	FeatureSecrets       Feature = "secrets"
	FeatureNotifications Feature = "notifications"
)

// AllFeatures is the canonical list of known features.
var AllFeatures = []Feature{
	FeatureWebhooks,
	FeatureSecrets,
	FeatureNotifications,
}

// ParseAllowedFeatures parses a comma-separated allowlist of feature names.
// "*" (or empty) means all features are enabled.
func ParseAllowedFeatures(input string) ([]Feature, error) {
	if input == "" || input == "*" {
		return AllFeatures, nil
	}

	parts := strings.Split(input, ",")
	features := make([]Feature, 0, len(parts))

	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}

		f := Feature(trimmed)
		if !isKnownFeature(f) {
			return nil, fmt.Errorf("unknown feature %q; known features: %s", trimmed, knownFeatureNames())
		}

		features = append(features, f)
	}

	if len(features) == 0 {
		return AllFeatures, nil
	}

	return features, nil
}

// IsFeatureEnabled checks whether the given feature is in the allowed set.
func IsFeatureEnabled(feature Feature, allowed []Feature) bool {
	for _, f := range allowed {
		if f == feature {
			return true
		}
	}

	return false
}

func isKnownFeature(f Feature) bool {
	for _, known := range AllFeatures {
		if known == f {
			return true
		}
	}

	return false
}

func knownFeatureNames() string {
	names := make([]string, len(AllFeatures))
	for i, f := range AllFeatures {
		names[i] = string(f)
	}

	return strings.Join(names, ", ")
}

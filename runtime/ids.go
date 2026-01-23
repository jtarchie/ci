package runtime

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	gonanoid "github.com/matoous/go-nanoid/v2"
)

// UniqueID generates a random unique identifier for things that should not be deterministic
// (e.g., run IDs, namespaces for fresh runs).
// This is a wrapper around gonanoid to consolidate random ID generation.
func UniqueID() string {
	return gonanoid.Must()
}

// DeterministicTaskID generates a deterministic task ID based on the execution context.
// Includes namespace and runID to prevent collisions across different pipeline runs.
// The stepID provides uniqueness within a run, and taskName provides semantic clarity.
//
// Returns an 8-character hexadecimal string.
func DeterministicTaskID(namespace, runID, stepID, taskName string) string {
	input := fmt.Sprintf("%s:%s:%s:%s", namespace, runID, stepID, taskName)
	hash := sha256.Sum256([]byte(input))

	return hex.EncodeToString(hash[:4]) // 4 bytes = 8 hex chars
}

// PipelineID generates a deterministic pipeline ID based on pipeline name and content.
// This enables caching and deduplication - identical pipelines will have identical IDs.
// Both name and content are included so different pipelines with the same content get unique IDs.
//
// Returns a 32-character hexadecimal string.
func PipelineID(name, content string) string {
	input := fmt.Sprintf("%s:%s", name, content)
	hash := sha256.Sum256([]byte(input))

	return hex.EncodeToString(hash[:16]) // 16 bytes = 32 hex chars
}

// DeterministicVolumeID generates a deterministic volume ID for unnamed volumes.
// Uses namespace and a context string to ensure unique but reproducible names.
//
// Returns an 8-character hexadecimal string.
func DeterministicVolumeID(namespace, context string) string {
	input := fmt.Sprintf("%s:%s", namespace, context)
	hash := sha256.Sum256([]byte(input))

	return hex.EncodeToString(hash[:4]) // 4 bytes = 8 hex chars
}

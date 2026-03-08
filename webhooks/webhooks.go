// Package webhooks provides a pluggable framework for detecting and parsing
// incoming webhook requests from multiple providers.
//
// Providers register themselves via init() using Add(), following the same
// self-registration pattern used by orchestra drivers. The Detect() function
// iterates the registry in registration order and returns the first match.
package webhooks

import (
	"errors"
	"net/http"
	"sort"
)

// ErrUnauthorized is returned by Parse when a signature check fails.
var ErrUnauthorized = errors.New("webhook signature validation failed")

// ErrNoMatch is returned by Detect when no registered provider matches the request.
var ErrNoMatch = errors.New("no webhook provider matched the request")

// Event is the normalized representation of an incoming webhook request,
// returned by a matched provider after signature verification.
type Event struct {
	Provider  string
	EventType string

	Method  string
	URL     string
	Headers map[string]string
	Body    string
	Query   map[string]string
}

// Provider is implemented by each webhook provider (github, slack, generic, …).
type Provider interface {
	// Name returns a stable identifier for the provider, e.g. "github".
	Name() string

	// Match reports whether this provider should handle the given request.
	// Implementations should inspect headers or other signals without consuming
	// the body (body bytes are supplied instead).
	Match(r *http.Request) bool

	// Parse verifies the request signature using secret (empty = skip
	// verification) and returns a normalised Event.
	// Returns ErrUnauthorized if signature verification fails.
	Parse(r *http.Request, body []byte, secret string) (*Event, error)
}

var registry []Provider

// Add registers a provider. Typically called from an init() function.
// The registry is kept sorted so that more-specific providers always run
// before catch-all providers: names are ordered alphabetically, except
// "generic" is always placed last so that specific providers (github, slack,
// …) take priority regardless of import order.
func Add(p Provider) {
	registry = append(registry, p)
	sort.SliceStable(registry, func(i, j int) bool {
		ni, nj := registry[i].Name(), registry[j].Name()
		if ni == "generic" {
			return false
		}
		if nj == "generic" {
			return true
		}
		return ni < nj
	})
}

// Reset clears all registered providers. Intended for use in tests.
func Reset() {
	registry = nil
}

// Detect iterates the registered providers and returns the Event from the
// first provider whose Match() returns true.
// Returns ErrNoMatch if no provider claims the request.
// Returns ErrUnauthorized (from the matched provider) on signature failure.
func Detect(r *http.Request, body []byte, secret string) (*Event, error) {
	for _, p := range registry {
		if p.Match(r) {
			return p.Parse(r, body, secret)
		}
	}

	return nil, ErrNoMatch
}

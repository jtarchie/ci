// Package filter evaluates webhook trigger expressions against webhook data.
// It uses github.com/expr-lang/expr for safe, sandboxed boolean expressions.
package filter

import (
	"fmt"

	"github.com/expr-lang/expr"
)

// WebhookEnv is the expression environment exposing webhook metadata.
// All fields are available by name in filter expressions.
type WebhookEnv struct {
	Provider  string            `expr:"provider"`
	EventType string            `expr:"eventType"`
	Method    string            `expr:"method"`
	Headers   map[string]string `expr:"headers"`
	Query     map[string]string `expr:"query"`
	Body      string            `expr:"body"`
	// Payload holds the JSON-decoded body. Nil when the body is not valid JSON.
	Payload map[string]any `expr:"payload"`
}

// Evaluate compiles and runs a boolean expression against env.
// Returns (false, error) when the expression is invalid or evaluation fails.
func Evaluate(expression string, env WebhookEnv) (bool, error) {
	program, err := expr.Compile(expression, expr.Env(env), expr.AsBool())
	if err != nil {
		return false, fmt.Errorf("webhook_trigger compile error: %w", err)
	}

	result, err := expr.Run(program, env)
	if err != nil {
		return false, fmt.Errorf("webhook_trigger eval error: %w", err)
	}

	return result.(bool), nil //nolint:forcetypeassert
}

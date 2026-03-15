package auth

import (
	"fmt"

	"github.com/expr-lang/expr"
)

// EvaluateAccess compiles and runs a boolean expression against the given user.
// Returns true if the expression is empty (no RBAC = full access).
// Returns (false, error) when the expression is invalid or evaluation fails.
func EvaluateAccess(expression string, user User) (bool, error) {
	if expression == "" {
		return true, nil
	}

	program, err := expr.Compile(expression, expr.Env(user), expr.AsBool())
	if err != nil {
		return false, fmt.Errorf("rbac compile error: %w", err)
	}

	result, err := expr.Run(program, user)
	if err != nil {
		return false, fmt.Errorf("rbac eval error: %w", err)
	}

	return result.(bool), nil //nolint:forcetypeassert
}

// ValidateExpression checks that an expr expression compiles correctly
// against the User type. Used for fail-fast validation when setting pipeline RBAC.
func ValidateExpression(expression string) error {
	if expression == "" {
		return nil
	}

	_, err := expr.Compile(expression, expr.Env(User{}), expr.AsBool())
	if err != nil {
		return fmt.Errorf("invalid rbac expression: %w", err)
	}

	return nil
}

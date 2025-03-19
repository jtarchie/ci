package runtime

import (
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"regexp"

	"github.com/dop251/goja"
	"github.com/onsi/gomega/format"
)

type Assert struct {
	logger *slog.Logger
	vm     *goja.Runtime
}

func NewAssert(vm *goja.Runtime, logger *slog.Logger) *Assert {
	logger.Debug("handler.created")

	return &Assert{
		logger: logger.WithGroup("assert"),
		vm:     vm,
	}
}

var ErrAssertion = errors.New("assertion failed")

func (a *Assert) fail(message string) {
	a.logger.Error("assertion.failed", "err", message)
	a.vm.Interrupt(fmt.Errorf("%w: %s", ErrAssertion, message))
}

func (a *Assert) Equal(expected, actual interface{}, message ...string) {
	a.logger.Debug("equality.checking",
		"expected_type", fmt.Sprintf("%T", expected),
		"actual_type", fmt.Sprintf("%T", actual))

	if !reflect.DeepEqual(actual, expected) {
		msg := format.Message(actual, "to be equivalent to", expected)

		if len(message) > 0 {
			msg = message[0]
		}

		a.fail(msg)
	}
}

func (a *Assert) NotEqual(expected, actual interface{}, message ...string) {
	a.logger.Debug("inequality.checking",
		"expected_type", fmt.Sprintf("%T", expected),
		"actual_type", fmt.Sprintf("%T", actual))

	if expected == actual {
		msg := fmt.Sprintf("expected not %v, but got %v", expected, actual)
		if len(message) > 0 {
			msg = message[0]
		}

		a.fail(msg)
	}
}

func (a *Assert) ContainsString(str, substr string, message ...string) {
	// Redact potentially sensitive string data in logs
	a.logger.Debug("substring.checking",
		"pattern_length", len(substr),
		"string_length", len(str))

	matcher, err := regexp.Compile(substr)
	if err != nil {
		a.logger.Debug("regex.failed", "err", err)
		a.fail(fmt.Sprintf("invalid regular expression: %s", err))

		return
	}

	if !matcher.MatchString(str) {
		msg := fmt.Sprintf("expected %q to contain %q", str, substr)
		if len(message) > 0 {
			msg = message[0]
		}

		a.fail(msg)
	}
}

func (a *Assert) Truthy(value bool, message ...string) {
	a.logger.Debug("truthiness.checking", "value_type", fmt.Sprintf("%T", value))

	if !value {
		msg := fmt.Sprintf("expected %v to be truthy", value)
		if len(message) > 0 {
			msg = message[0]
		}

		a.fail(msg)
	}
}

func (a *Assert) ContainsElement(array []interface{}, element interface{}, message ...string) {
	a.logger.Debug("element.checking",
		"element_type", fmt.Sprintf("%T", element),
		"array_length", len(array))

	found := false

	for _, item := range array {
		if item == element {
			found = true

			break
		}
	}

	if !found {
		msg := fmt.Sprintf("expected array to contain %v", element)
		if len(message) > 0 {
			msg = message[0]
		}

		a.fail(msg)
	}
}

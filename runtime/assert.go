package runtime

import (
	"errors"
	"fmt"
	"reflect"
	"regexp"

	"github.com/dop251/goja"
	"github.com/onsi/gomega/format"
)

type Assert struct {
	vm *goja.Runtime
}

func NewAssert(vm *goja.Runtime) *Assert {
	return &Assert{vm: vm}
}

var ErrAssertion = errors.New("assertion failed")

func (a *Assert) fail(message string) {
	a.vm.Interrupt(fmt.Errorf("%w: %s", ErrAssertion, message))
}

func (a *Assert) Equal(expected, actual interface{}, message ...string) {
	if !reflect.DeepEqual(actual, expected) {
		msg := format.Message(actual, "to be equivalent to", expected)

		if len(message) > 0 {
			msg = message[0]
		}

		a.fail(msg)
	}
}

func (a *Assert) NotEqual(expected, actual interface{}, message ...string) {
	if expected == actual {
		msg := fmt.Sprintf("expected not %v, but got %v", expected, actual)
		if len(message) > 0 {
			msg = message[0]
		}

		a.fail(msg)
	}
}

func (a *Assert) ContainsString(substr, str string, message ...string) {
	matcher, err := regexp.Compile(substr)
	if err != nil {
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

func (a *Assert) Truthy(value interface{}, message ...string) {
	if value == false {
		msg := fmt.Sprintf("expected %v to be truthy", value)
		if len(message) > 0 {
			msg = message[0]
		}

		a.fail(msg)
	}
}

func (a *Assert) ContainsElement(element interface{}, array []interface{}, message ...string) {
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

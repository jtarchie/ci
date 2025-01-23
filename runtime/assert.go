package runtime

import (
	"fmt"
	"regexp"

	"github.com/dop251/goja"
)

type Assert struct {
	vm *goja.Runtime
}

func NewAssert(vm *goja.Runtime) *Assert {
	return &Assert{vm: vm}
}

func (a *Assert) Fail(message string) {
	a.vm.Interrupt(fmt.Errorf("Assertion failed: %s", message))
}

func (a *Assert) Equal(expected, actual interface{}) {
	if expected != actual {
		a.Fail(fmt.Sprintf("expected %v, but got %v", expected, actual))
	}
}

func (a *Assert) NotEqual(expected, actual interface{}) {
	if expected == actual {
		a.Fail(fmt.Sprintf("expected not %v, but got %v", expected, actual))
	}
}

func (a *Assert) ContainsString(substr, str string) {
	re, err := regexp.Compile(substr)
	if err != nil {
		a.Fail(fmt.Sprintf("invalid regular expression: %s", err))
		return
	}

	if !re.MatchString(str) {
		a.Fail(fmt.Sprintf("expected %q to contain %q", str, substr))
	}
}

func (a *Assert) ContainsElement(element interface{}, array []interface{}) {
	found := false
	for _, item := range array {
		if item == element {
			found = true
			break
		}
	}
	if !found {
		a.Fail(fmt.Sprintf("expected array to contain %v", element))
	}
}

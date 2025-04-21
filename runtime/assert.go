package runtime

import (
	"bufio"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"regexp"
	"time"

	"github.com/davecgh/go-spew/spew"
	"github.com/dop251/goja"
	"github.com/pmezard/go-difflib/difflib"
)

// MaxDepth defines the maximum depth for spew dumping.
const (
	MaxDepth      = 10
	SpacingMargin = 100
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

func (a *Assert) Equal(expected, actual interface{}, message ...string) {
	a.logger.Debug("equality.checking",
		"expected_type", fmt.Sprintf("%T", expected),
		"actual_type", fmt.Sprintf("%T", actual))

	if !reflect.DeepEqual(actual, expected) {
		diff := diff(expected, actual)
		expected, actual = formatUnequalValues(expected, actual)
		msg := fmt.Sprintf("Not equal: \n"+
			"expected: %s\n"+
			"actual  : %s%s", expected, actual, diff)

		if len(message) > 0 {
			msg = message[0] + "\n" + msg
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

func (a *Assert) fail(message string) {
	a.logger.Error("assertion.failed", "err", message)
	a.vm.Interrupt(fmt.Errorf("%w: %s", ErrAssertion, message))
}

var spewConfig = spew.ConfigState{
	Indent:                  " ",
	DisablePointerAddresses: true,
	DisableCapacities:       true,
	SortKeys:                true,
	DisableMethods:          true,
	MaxDepth:                MaxDepth,
}

var spewConfigStringerEnabled = spew.ConfigState{
	Indent:                  " ",
	DisablePointerAddresses: true,
	DisableCapacities:       true,
	SortKeys:                true,
	MaxDepth:                MaxDepth,
}

func typeAndKind(v interface{}) (reflect.Type, reflect.Kind) {
	valType := reflect.TypeOf(v)
	valKind := valType.Kind()

	if valKind == reflect.Ptr {
		valType = valType.Elem()
		valKind = valType.Kind()
	}

	return valType, valKind
}

func diff(expected interface{}, actual interface{}) string {
	if expected == nil || actual == nil {
		return ""
	}

	expectedType, expectedKind := typeAndKind(expected)
	actualType, _ := typeAndKind(actual)

	if expectedType != actualType {
		return ""
	}

	if expectedKind != reflect.Struct && expectedKind != reflect.Map && expectedKind != reflect.Slice && expectedKind != reflect.Array && expectedKind != reflect.String {
		return ""
	}

	var expectedStr, actualStr string

	switch expectedType {
	case reflect.TypeOf(""):
		expectedStr = reflect.ValueOf(expected).String()
		actualStr = reflect.ValueOf(actual).String()
	case reflect.TypeOf(time.Time{}):
		expectedStr = spewConfigStringerEnabled.Sdump(expected)
		actualStr = spewConfigStringerEnabled.Sdump(actual)
	default:
		expectedStr = spewConfig.Sdump(expected)
		actualStr = spewConfig.Sdump(actual)
	}

	diff, _ := difflib.GetUnifiedDiffString(difflib.UnifiedDiff{
		A:        difflib.SplitLines(expectedStr),
		B:        difflib.SplitLines(actualStr),
		FromFile: "Expected",
		FromDate: "",
		ToFile:   "Actual",
		ToDate:   "",
		Context:  1,
	})

	return "\n\nDiff:\n" + diff
}

func formatUnequalValues(expected, actual interface{}) (string, string) {
	if reflect.TypeOf(expected) != reflect.TypeOf(actual) {
		return fmt.Sprintf("%T(%s)", expected, truncatingFormat(expected)),
			fmt.Sprintf("%T(%s)", actual, truncatingFormat(actual))
	}

	if _, ok := expected.(time.Duration); ok {
		return fmt.Sprintf("%v", expected), fmt.Sprintf("%v", actual)
	}

	return truncatingFormat(expected), truncatingFormat(actual)
}

func truncatingFormat(data interface{}) string {
	value := fmt.Sprintf("%#v", data)
	maxCap := bufio.MaxScanTokenSize - SpacingMargin // Give us some space the type info too if needed.

	if len(value) > maxCap {
		value = value[0:maxCap] + "<... truncated>"
	}

	return value
}

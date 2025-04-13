package runtime_test

import (
	"log/slog"
	"strings"
	"testing"

	"github.com/dop251/goja"
	"github.com/jtarchie/ci/runtime"
	. "github.com/onsi/gomega"
)

func TestAssert(t *testing.T) {
	t.Parallel()

	assert := NewGomegaWithT(t)

	jsVM := goja.New()
	err := jsVM.Set("assert", runtime.NewAssert(jsVM, slog.Default()))
	assert.Expect(err).NotTo(HaveOccurred())
	_, err = jsVM.RunString(strings.TrimSpace(`
		assert.Equal(1, 1);
		assert.NotEqual(1, 2);
		assert.ContainsString("foobar", "foo");
		assert.Truthy(true);
		assert.ContainsElement([1, 2, 3], 2);
	`))
	assert.Expect(err).NotTo(HaveOccurred())

	failures := []string{
		"assert.Equal(1, 2);",
		"assert.NotEqual(1, 1);",
		"assert.ContainsString('foobar', 'baz');",
		"assert.Truthy(false);",
		"assert.ContainsElement([1, 2, 3], 4);",
	}

	for _, failure := range failures {
		_, err = jsVM.RunString(strings.TrimSpace(failure))
		assert.Expect(err).To(HaveOccurred(), "expected error for "+failure)

		errString := err.Error()
		assert.Expect(errString).To(ContainSubstring("assertion failed"))
	}
}

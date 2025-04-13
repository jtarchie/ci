package runtime_test

import (
	"log/slog"
	"strings"
	"testing"

	"github.com/dop251/goja"
	"github.com/jtarchie/ci/runtime"
	. "github.com/onsi/gomega"
)

func TestYAML(t *testing.T) {
	t.Parallel()

	assert := NewGomegaWithT(t)

	jsVM := goja.New()
	err := jsVM.Set("assert", runtime.NewAssert(jsVM, slog.Default()))
	assert.Expect(err).NotTo(HaveOccurred())

	err = jsVM.Set("YAML", runtime.NewYAML(jsVM, slog.Default()))
	assert.Expect(err).NotTo(HaveOccurred())

	_, err = jsVM.RunString(strings.TrimSpace(`
		const payload = YAML.Parse("foo: bar\nbaz: qux");
		assert.Equal(payload.foo, "bar");
		assert.Equal(payload.baz, "qux");
		
		const yaml = YAML.Stringify(payload);
		assert.Equal(yaml, "baz: qux\nfoo: bar\n");
	`))
	assert.Expect(err).NotTo(HaveOccurred())

	_, err = jsVM.RunString(`YAML.Parse("[{]]")`)
	assert.Expect(err).To(HaveOccurred())
}

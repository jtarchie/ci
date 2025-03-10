package runtime_test

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/jtarchie/ci/runtime"
	. "github.com/onsi/gomega"
)

func TestBrokenJS(t *testing.T) {
	t.Parallel()

	assert := NewGomegaWithT(t)

	js := runtime.NewJS(context.Background(), slog.Default())
	err := js.Execute(strings.TrimSpace(`
		export function pipeline() {
			const array = [];
			return array[1].asdf;
		};
	`), nil)
	assert.Expect(err).To(HaveOccurred())
	assert.Expect(err.Error()).To(ContainSubstring("main.js:3"))
}

func TestAwaitPromise(t *testing.T) {
	t.Parallel()

	assert := NewGomegaWithT(t)

	js := runtime.NewJS(context.Background(), slog.Default())
	err := js.Execute(`
		async function pipeline() {
			await Promise.reject(400);
		};

		export { pipeline };
	`, nil)
	assert.Expect(err).To(HaveOccurred())
}

func TestUseContext(t *testing.T) {
	t.Parallel()

	assert := NewGomegaWithT(t)

	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()

	js := runtime.NewJS(ctx, slog.Default())
	err := js.Execute(`
		function pipeline() {
			for (; true; ) {}
		};

		export { pipeline };
	`, nil)
	assert.Expect(err).To(HaveOccurred())
}

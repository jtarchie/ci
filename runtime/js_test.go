package runtime_test

import (
	"context"
	"strings"
	"testing"

	"github.com/jtarchie/ci/runtime"
	. "github.com/onsi/gomega"
)

func TestBrokenJS(t *testing.T) {
	t.Parallel()

	assert := NewGomegaWithT(t)

	js := runtime.NewJS(context.Background())
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

	js := runtime.NewJS(context.Background())
	err := js.Execute(`
		async function pipeline() {
			await Promise.reject(400);
		};

		export { pipeline };
	`, nil)
	assert.Expect(err).To(HaveOccurred())
}

package runtime_test

import (
	"testing"

	"github.com/jtarchie/ci/runtime"
	. "github.com/onsi/gomega"
)

func TestBrokenJS(t *testing.T) {
	t.Parallel()

	assert := NewGomegaWithT(t)

	js := runtime.NewJS()
	err := js.Execute(`
		export function pipeline() {
			const array = [];
			return array[1].asdf;
		};
	`, nil)
	assert.Expect(err).To(HaveOccurred())
}

func TestAwaitPromise(t *testing.T) {
	t.Parallel()

	assert := NewGomegaWithT(t)

	js := runtime.NewJS()
	err := js.Execute(`
		async function pipeline() {
			await Promise.reject(400);
		};

		export { pipeline };
	`, nil)
	assert.Expect(err).To(HaveOccurred())
}

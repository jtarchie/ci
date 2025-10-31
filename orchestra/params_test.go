package orchestra_test

import (
	"os"
	"testing"

	"github.com/jtarchie/ci/orchestra"
	. "github.com/onsi/gomega"
)

func TestGetParam(t *testing.T) {
	t.Run("returns DSN parameter when present", func(t *testing.T) {
		assert := NewGomegaWithT(t)

		params := map[string]string{"key": "dsn-value"}
		result := orchestra.GetParam(params, "key", "ENV_VAR", "default")

		assert.Expect(result).To(Equal("dsn-value"))
	})

	t.Run("falls back to environment variable when DSN param is empty", func(t *testing.T) {
		assert := NewGomegaWithT(t)

		assert.Expect(os.Setenv("TEST_ENV_VAR", "env-value")).To(Succeed()) //nolint:tenv
		defer os.Unsetenv("TEST_ENV_VAR")                                   //nolint:tenv

		params := map[string]string{}
		result := orchestra.GetParam(params, "key", "TEST_ENV_VAR", "default")

		assert.Expect(result).To(Equal("env-value"))
	})

	t.Run("returns default value when neither DSN param nor env var exist", func(t *testing.T) {
		assert := NewGomegaWithT(t)

		params := map[string]string{}
		result := orchestra.GetParam(params, "key", "NONEXISTENT_ENV_VAR", "default-value")

		assert.Expect(result).To(Equal("default-value"))
	})

	t.Run("prefers DSN parameter over environment variable", func(t *testing.T) {
		assert := NewGomegaWithT(t)

		assert.Expect(os.Setenv("TEST_ENV_VAR_2", "env-value")).To(Succeed()) //nolint:tenv
		defer os.Unsetenv("TEST_ENV_VAR_2")                                   //nolint:tenv

		params := map[string]string{"key": "dsn-value"}
		result := orchestra.GetParam(params, "key", "TEST_ENV_VAR_2", "default")

		assert.Expect(result).To(Equal("dsn-value"))
	})

	t.Run("works with empty env var name", func(t *testing.T) {
		assert := NewGomegaWithT(t)

		params := map[string]string{}
		result := orchestra.GetParam(params, "key", "", "default-value")

		assert.Expect(result).To(Equal("default-value"))
	})
}

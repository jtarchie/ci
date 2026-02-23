package runtime_test

import (
	"testing"

	"github.com/jtarchie/ci/runtime"
	. "github.com/onsi/gomega"
)

func TestRedactSecrets(t *testing.T) {
	t.Parallel()

	t.Run("simple redaction", func(t *testing.T) {
		t.Parallel()

		assert := NewGomegaWithT(t)

		result := runtime.RedactSecrets("my password is secret123", []string{"secret123"})
		assert.Expect(result).To(Equal("my password is ***REDACTED***"))
	})

	t.Run("multiple secrets", func(t *testing.T) {
		t.Parallel()

		assert := NewGomegaWithT(t)

		result := runtime.RedactSecrets("key=abc token=xyz", []string{"abc", "xyz"})
		assert.Expect(result).To(Equal("key=***REDACTED*** token=***REDACTED***"))
	})

	t.Run("longest match first", func(t *testing.T) {
		t.Parallel()

		assert := NewGomegaWithT(t)

		// "secret" is a substring of "secret123" - the longer one should be replaced first
		result := runtime.RedactSecrets("the value is secret123", []string{"secret", "secret123"})
		assert.Expect(result).To(Equal("the value is ***REDACTED***"))
	})

	t.Run("special regex characters", func(t *testing.T) {
		t.Parallel()

		assert := NewGomegaWithT(t)

		result := runtime.RedactSecrets("value is $DOLLAR.*STAR", []string{"$DOLLAR", ".*STAR"})
		assert.Expect(result).To(Equal("value is ***REDACTED******REDACTED***"))
	})

	t.Run("no secrets", func(t *testing.T) {
		t.Parallel()

		assert := NewGomegaWithT(t)

		result := runtime.RedactSecrets("nothing to redact", []string{})
		assert.Expect(result).To(Equal("nothing to redact"))
	})

	t.Run("empty text", func(t *testing.T) {
		t.Parallel()

		assert := NewGomegaWithT(t)

		result := runtime.RedactSecrets("", []string{"secret"})
		assert.Expect(result).To(Equal(""))
	})

	t.Run("empty secret values skipped", func(t *testing.T) {
		t.Parallel()

		assert := NewGomegaWithT(t)

		result := runtime.RedactSecrets("hello world", []string{"", ""})
		assert.Expect(result).To(Equal("hello world"))
	})

	t.Run("repeated occurrences", func(t *testing.T) {
		t.Parallel()

		assert := NewGomegaWithT(t)

		result := runtime.RedactSecrets("pass=abc pass=abc pass=abc", []string{"abc"})
		assert.Expect(result).To(Equal("pass=***REDACTED*** pass=***REDACTED*** pass=***REDACTED***"))
	})

	t.Run("multiline text", func(t *testing.T) {
		t.Parallel()

		assert := NewGomegaWithT(t)

		text := "line1: secret-val\nline2: other\nline3: secret-val\n"
		result := runtime.RedactSecrets(text, []string{"secret-val"})
		assert.Expect(result).To(Equal("line1: ***REDACTED***\nline2: other\nline3: ***REDACTED***\n"))
	})

	t.Run("backslash in secret", func(t *testing.T) {
		t.Parallel()

		assert := NewGomegaWithT(t)

		result := runtime.RedactSecrets(`path is C:\Users\admin`, []string{`C:\Users\admin`})
		assert.Expect(result).To(Equal("path is ***REDACTED***"))
	})
}

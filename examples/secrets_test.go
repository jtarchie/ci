package main_test

import (
	"path/filepath"
	"testing"

	"github.com/jtarchie/ci/commands"
	_ "github.com/jtarchie/ci/orchestra/docker"
	_ "github.com/jtarchie/ci/orchestra/native"
	_ "github.com/jtarchie/ci/secrets/local"
	_ "github.com/jtarchie/ci/storage/sqlite"
	. "github.com/onsi/gomega"
)

func TestSecretsBasic(t *testing.T) {
	t.Parallel()

	drivers := []string{"docker", "native"}

	for _, driver := range drivers {
		t.Run(driver, func(t *testing.T) {
			t.Parallel()

			assert := NewGomegaWithT(t)

			examplePath, err := filepath.Abs("both/secrets-basic.ts")
			assert.Expect(err).NotTo(HaveOccurred())

			runner := commands.Runner{
				Pipeline: examplePath,
				Driver:   driver,
				Storage:  "sqlite://:memory:",
				Secrets:  "local://:memory:?key=test-passphrase",
				Secret:   []string{"API_KEY=super-secret-value-12345"},
			}
			err = runner.Run(nil)
			assert.Expect(err).NotTo(HaveOccurred())
		})
	}
}

func TestSecretsMissingFails(t *testing.T) {
	t.Parallel()

	assert := NewGomegaWithT(t)

	examplePath, err := filepath.Abs("both/secrets-basic.ts")
	assert.Expect(err).NotTo(HaveOccurred())

	runner := commands.Runner{
		Pipeline: examplePath,
		Driver:   "native",
		Storage:  "sqlite://:memory:",
		Secrets:  "local://:memory:?key=test-passphrase",
	}
	err = runner.Run(nil)
	assert.Expect(err).To(HaveOccurred())
	assert.Expect(err.Error()).To(ContainSubstring("API_KEY"))
}

func TestSecretsInvalidFlag(t *testing.T) {
	t.Parallel()

	assert := NewGomegaWithT(t)

	examplePath, err := filepath.Abs("both/secrets-basic.ts")
	assert.Expect(err).NotTo(HaveOccurred())

	runner := commands.Runner{
		Pipeline: examplePath,
		Driver:   "native",
		Storage:  "sqlite://:memory:",
		Secrets:  "local://:memory:?key=test-passphrase",
		Secret:   []string{"INVALID_NO_EQUALS"},
	}
	err = runner.Run(nil)
	assert.Expect(err).To(HaveOccurred())
	assert.Expect(err.Error()).To(ContainSubstring("expected KEY=VALUE"))
}

package mock_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/jtarchie/ci/resources"
	_ "github.com/jtarchie/ci/resources/mock"
	. "github.com/onsi/gomega"
)

func TestMockResource(t *testing.T) {
	t.Run("is registered", func(t *testing.T) {
		assert := NewGomegaWithT(t)

		assert.Expect(resources.IsNative("mock")).To(BeTrue())

		res, err := resources.Get("mock")
		assert.Expect(err).NotTo(HaveOccurred())
		assert.Expect(res.Name()).To(Equal("mock"))
	})

	t.Run("check returns force_version when specified", func(t *testing.T) {
		assert := NewGomegaWithT(t)

		res, err := resources.Get("mock")
		assert.Expect(err).NotTo(HaveOccurred())

		ctx := context.Background()
		resp, err := res.Check(ctx, resources.CheckRequest{
			Source: map[string]interface{}{
				"force_version": "test-version",
			},
		})
		assert.Expect(err).NotTo(HaveOccurred())
		assert.Expect(resp).To(HaveLen(1))
		assert.Expect(resp[0]["version"]).To(Equal("test-version"))
	})

	t.Run("in creates version file", func(t *testing.T) {
		assert := NewGomegaWithT(t)

		res, err := resources.Get("mock")
		assert.Expect(err).NotTo(HaveOccurred())

		destDir, err := os.MkdirTemp("", "mock-in-test-*")
		assert.Expect(err).NotTo(HaveOccurred())

		defer func() {
			_ = os.RemoveAll(destDir)
		}()

		ctx := context.Background()
		inResp, err := res.In(ctx, destDir, resources.InRequest{
			Source: map[string]interface{}{},
			Version: resources.Version{
				"version": "1.0.0",
			},
		})
		assert.Expect(err).NotTo(HaveOccurred())
		assert.Expect(inResp.Version["version"]).To(Equal("1.0.0"))

		// Verify version file was created
		versionFile := filepath.Join(destDir, "version")
		content, err := os.ReadFile(versionFile)
		assert.Expect(err).NotTo(HaveOccurred())
		assert.Expect(string(content)).To(Equal("1.0.0"))
	})

	t.Run("out returns version from params", func(t *testing.T) {
		assert := NewGomegaWithT(t)

		res, err := resources.Get("mock")
		assert.Expect(err).NotTo(HaveOccurred())

		ctx := context.Background()
		outResp, err := res.Out(ctx, "/tmp", resources.OutRequest{
			Source: map[string]interface{}{},
			Params: map[string]interface{}{
				"version": "2.0.0",
			},
		})
		assert.Expect(err).NotTo(HaveOccurred())
		assert.Expect(outResp.Version["version"]).To(Equal("2.0.0"))
	})
}

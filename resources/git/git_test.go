package git_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/jtarchie/ci/resources"
	_ "github.com/jtarchie/ci/resources/git"
	. "github.com/onsi/gomega"
)

func TestGitResource(t *testing.T) {
	t.Run("is registered", func(t *testing.T) {
		assert := NewGomegaWithT(t)

		assert.Expect(resources.IsNative("git")).To(BeTrue())

		res, err := resources.Get("git")
		assert.Expect(err).NotTo(HaveOccurred())
		assert.Expect(res.Name()).To(Equal("git"))
	})

	t.Run("check returns latest version for public repo", func(t *testing.T) {
		assert := NewGomegaWithT(t)

		res, err := resources.Get("git")
		assert.Expect(err).NotTo(HaveOccurred())

		ctx := context.Background()
		resp, err := res.Check(ctx, resources.CheckRequest{
			Source: map[string]interface{}{
				"uri":    "https://github.com/octocat/Hello-World.git",
				"branch": "master",
			},
		})
		assert.Expect(err).NotTo(HaveOccurred())
		assert.Expect(resp).NotTo(BeEmpty())
		assert.Expect(resp[0]["ref"]).NotTo(BeEmpty())
	})

	t.Run("in clones repository at specific version", func(t *testing.T) {
		assert := NewGomegaWithT(t)

		res, err := resources.Get("git")
		assert.Expect(err).NotTo(HaveOccurred())

		// First, check to get a version
		ctx := context.Background()
		checkResp, err := res.Check(ctx, resources.CheckRequest{
			Source: map[string]interface{}{
				"uri":    "https://github.com/octocat/Hello-World.git",
				"branch": "master",
			},
		})
		assert.Expect(err).NotTo(HaveOccurred())
		assert.Expect(checkResp).NotTo(BeEmpty())

		// Create temp directory for clone
		destDir, err := os.MkdirTemp("", "git-in-test-*")
		assert.Expect(err).NotTo(HaveOccurred())

		defer func() {
			_ = os.RemoveAll(destDir)
		}()

		// In (get) the repository
		inResp, err := res.In(ctx, destDir, resources.InRequest{
			Source: map[string]interface{}{
				"uri":    "https://github.com/octocat/Hello-World.git",
				"branch": "master",
			},
			Version: checkResp[0],
		})
		assert.Expect(err).NotTo(HaveOccurred())
		assert.Expect(inResp.Version["ref"]).To(Equal(checkResp[0]["ref"]))
		assert.Expect(inResp.Metadata).NotTo(BeEmpty())

		// Verify README exists
		readmePath := filepath.Join(destDir, "README")
		_, err = os.Stat(readmePath)
		assert.Expect(err).NotTo(HaveOccurred())
	})
}

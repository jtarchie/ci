package resources_test

import (
	"testing"

	"github.com/jtarchie/ci/resources"
	_ "github.com/jtarchie/ci/resources/git"
	_ "github.com/jtarchie/ci/resources/mock"
	. "github.com/onsi/gomega"
)

func TestRegistry(t *testing.T) {
	t.Run("List returns registered resources", func(t *testing.T) {
		assert := NewGomegaWithT(t)

		list := resources.List()
		assert.Expect(list).To(ContainElement("git"))
		assert.Expect(list).To(ContainElement("mock"))
	})

	t.Run("Get returns error for unknown resource", func(t *testing.T) {
		assert := NewGomegaWithT(t)

		_, err := resources.Get("nonexistent-resource-type")
		assert.Expect(err).To(HaveOccurred())
		assert.Expect(err.Error()).To(ContainSubstring("unknown resource type"))
	})

	t.Run("IsNative returns true for registered resource", func(t *testing.T) {
		assert := NewGomegaWithT(t)

		assert.Expect(resources.IsNative("git")).To(BeTrue())
		assert.Expect(resources.IsNative("mock")).To(BeTrue())
	})

	t.Run("IsNative returns false for unknown resource", func(t *testing.T) {
		assert := NewGomegaWithT(t)

		assert.Expect(resources.IsNative("nonexistent")).To(BeFalse())
	})

	t.Run("Get returns a valid resource", func(t *testing.T) {
		assert := NewGomegaWithT(t)

		gitResource, err := resources.Get("git")
		assert.Expect(err).NotTo(HaveOccurred())
		assert.Expect(gitResource).NotTo(BeNil())
		assert.Expect(gitResource.Name()).To(Equal("git"))

		mockResource, err := resources.Get("mock")
		assert.Expect(err).NotTo(HaveOccurred())
		assert.Expect(mockResource).NotTo(BeNil())
		assert.Expect(mockResource.Name()).To(Equal("mock"))
	})
}

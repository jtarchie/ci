package server

import (
	"testing"

	. "github.com/onsi/gomega"
)

func TestParseAllowedFeatures(t *testing.T) {
	t.Parallel()

	t.Run("wildcard returns all features", func(t *testing.T) {
		t.Parallel()
		assert := NewGomegaWithT(t)

		features, err := ParseAllowedFeatures("*")
		assert.Expect(err).NotTo(HaveOccurred())
		assert.Expect(features).To(ConsistOf(FeatureWebhooks, FeatureSecrets, FeatureNotifications, FeatureFetch))
	})

	t.Run("empty returns all features", func(t *testing.T) {
		t.Parallel()
		assert := NewGomegaWithT(t)

		features, err := ParseAllowedFeatures("")
		assert.Expect(err).NotTo(HaveOccurred())
		assert.Expect(features).To(ConsistOf(FeatureWebhooks, FeatureSecrets, FeatureNotifications, FeatureFetch))
	})

	t.Run("single feature", func(t *testing.T) {
		t.Parallel()
		assert := NewGomegaWithT(t)

		features, err := ParseAllowedFeatures("webhooks")
		assert.Expect(err).NotTo(HaveOccurred())
		assert.Expect(features).To(Equal([]Feature{FeatureWebhooks}))
	})

	t.Run("multiple features", func(t *testing.T) {
		t.Parallel()
		assert := NewGomegaWithT(t)

		features, err := ParseAllowedFeatures("webhooks,secrets")
		assert.Expect(err).NotTo(HaveOccurred())
		assert.Expect(features).To(ConsistOf(FeatureWebhooks, FeatureSecrets))
	})

	t.Run("trims whitespace", func(t *testing.T) {
		t.Parallel()
		assert := NewGomegaWithT(t)

		features, err := ParseAllowedFeatures(" webhooks , secrets ")
		assert.Expect(err).NotTo(HaveOccurred())
		assert.Expect(features).To(ConsistOf(FeatureWebhooks, FeatureSecrets))
	})

	t.Run("unknown feature returns error", func(t *testing.T) {
		t.Parallel()
		assert := NewGomegaWithT(t)

		_, err := ParseAllowedFeatures("webhooks,bogus")
		assert.Expect(err).To(HaveOccurred())
		assert.Expect(err.Error()).To(ContainSubstring("unknown feature"))
		assert.Expect(err.Error()).To(ContainSubstring("bogus"))
	})
}

func TestIsFeatureEnabled(t *testing.T) {
	t.Parallel()

	t.Run("returns true when feature is in allowed list", func(t *testing.T) {
		t.Parallel()
		assert := NewGomegaWithT(t)

		allowed := []Feature{FeatureWebhooks, FeatureSecrets}
		assert.Expect(IsFeatureEnabled(FeatureWebhooks, allowed)).To(BeTrue())
		assert.Expect(IsFeatureEnabled(FeatureSecrets, allowed)).To(BeTrue())
	})

	t.Run("returns false when feature is not in allowed list", func(t *testing.T) {
		t.Parallel()
		assert := NewGomegaWithT(t)

		allowed := []Feature{FeatureWebhooks}
		assert.Expect(IsFeatureEnabled(FeatureSecrets, allowed)).To(BeFalse())
		assert.Expect(IsFeatureEnabled(FeatureNotifications, allowed)).To(BeFalse())
	})

	t.Run("returns false for empty allowed list", func(t *testing.T) {
		t.Parallel()
		assert := NewGomegaWithT(t)

		assert.Expect(IsFeatureEnabled(FeatureWebhooks, []Feature{})).To(BeFalse())
	})
}

package s3config_test

import (
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/jtarchie/pocketci/s3config"
	. "github.com/onsi/gomega"
)

func TestParseDSN_Minimal(t *testing.T) {
	assert := NewGomegaWithT(t)

	cfg, err := s3config.ParseDSN("s3://mybucket")
	assert.Expect(err).NotTo(HaveOccurred())
	assert.Expect(cfg.Bucket).To(Equal("mybucket"))
	assert.Expect(cfg.Prefix).To(Equal(""))
	assert.Expect(cfg.Region).To(Equal(""))
	assert.Expect(cfg.Endpoint).To(Equal(""))
	assert.Expect(cfg.ForcePathStyle).To(BeFalse())
	assert.Expect(cfg.SSE).To(BeEmpty())
	assert.Expect(cfg.SSEKMSKeyID).To(Equal(""))
	assert.Expect(cfg.TTL).To(Equal(time.Duration(0)))
}

func TestParseDSN_WithPrefix(t *testing.T) {
	assert := NewGomegaWithT(t)

	cfg, err := s3config.ParseDSN("s3://mybucket/some/prefix")
	assert.Expect(err).NotTo(HaveOccurred())
	assert.Expect(cfg.Bucket).To(Equal("mybucket"))
	assert.Expect(cfg.Prefix).To(Equal("some/prefix"))
}

func TestParseDSN_WithRegion(t *testing.T) {
	assert := NewGomegaWithT(t)

	cfg, err := s3config.ParseDSN("s3://mybucket?region=us-west-2")
	assert.Expect(err).NotTo(HaveOccurred())
	assert.Expect(cfg.Region).To(Equal("us-west-2"))
	assert.Expect(cfg.ForcePathStyle).To(BeFalse())
}

func TestParseDSN_WithEndpoint_AutoPathStyle(t *testing.T) {
	assert := NewGomegaWithT(t)

	cfg, err := s3config.ParseDSN("s3://mybucket?endpoint=http://localhost:9000&region=us-east-1")
	assert.Expect(err).NotTo(HaveOccurred())
	assert.Expect(cfg.Endpoint).To(Equal("http://localhost:9000"))
	// ForcePathStyle automatically true when endpoint is set
	assert.Expect(cfg.ForcePathStyle).To(BeTrue())
}

func TestParseDSN_ForcePathStyleFalse(t *testing.T) {
	assert := NewGomegaWithT(t)

	// R2 or providers that require virtual-hosted style
	cfg, err := s3config.ParseDSN("s3://mybucket?endpoint=https://account.r2.cloudflarestorage.com&force_path_style=false")
	assert.Expect(err).NotTo(HaveOccurred())
	assert.Expect(cfg.Endpoint).NotTo(BeEmpty())
	assert.Expect(cfg.ForcePathStyle).To(BeFalse())
}

func TestParseDSN_SSE_AES256(t *testing.T) {
	assert := NewGomegaWithT(t)

	cfg, err := s3config.ParseDSN("s3://mybucket?sse=AES256")
	assert.Expect(err).NotTo(HaveOccurred())
	assert.Expect(cfg.SSE).To(Equal(types.ServerSideEncryptionAes256))
	assert.Expect(cfg.SSEKMSKeyID).To(Equal(""))
}

func TestParseDSN_SSE_KMS_WithoutKey(t *testing.T) {
	assert := NewGomegaWithT(t)

	// Allow aws:kms without a key ID — provider uses its default KMS key.
	cfg, err := s3config.ParseDSN("s3://mybucket?sse=aws:kms")
	assert.Expect(err).NotTo(HaveOccurred())
	assert.Expect(cfg.SSE).To(Equal(types.ServerSideEncryptionAwsKms))
	assert.Expect(cfg.SSEKMSKeyID).To(Equal(""))
}

func TestParseDSN_SSE_KMS_WithKey(t *testing.T) {
	assert := NewGomegaWithT(t)

	cfg, err := s3config.ParseDSN("s3://mybucket?sse=aws:kms&sse_kms_key_id=arn:aws:kms:us-east-1:123456789012:key/mrk-abc123")
	assert.Expect(err).NotTo(HaveOccurred())
	assert.Expect(cfg.SSE).To(Equal(types.ServerSideEncryptionAwsKms))
	assert.Expect(cfg.SSEKMSKeyID).To(Equal("arn:aws:kms:us-east-1:123456789012:key/mrk-abc123"))
}

func TestParseDSN_SSE_InvalidValue(t *testing.T) {
	assert := NewGomegaWithT(t)

	_, err := s3config.ParseDSN("s3://mybucket?sse=SSE-C")
	assert.Expect(err).To(HaveOccurred())
	assert.Expect(err.Error()).To(ContainSubstring("unsupported sse value"))
}

func TestParseDSN_TTL(t *testing.T) {
	assert := NewGomegaWithT(t)

	cfg, err := s3config.ParseDSN("s3://mybucket?ttl=24h")
	assert.Expect(err).NotTo(HaveOccurred())
	assert.Expect(cfg.TTL).To(Equal(24 * time.Hour))
}

func TestParseDSN_TTL_Invalid(t *testing.T) {
	assert := NewGomegaWithT(t)

	_, err := s3config.ParseDSN("s3://mybucket?ttl=notaduration")
	assert.Expect(err).To(HaveOccurred())
	assert.Expect(err.Error()).To(ContainSubstring("failed to parse ttl"))
}

func TestParseDSN_WrongScheme(t *testing.T) {
	assert := NewGomegaWithT(t)

	_, err := s3config.ParseDSN("https://mybucket")
	assert.Expect(err).To(HaveOccurred())
	assert.Expect(err.Error()).To(ContainSubstring("expected s3://"))
}

func TestParseDSN_MissingBucket(t *testing.T) {
	assert := NewGomegaWithT(t)

	_, err := s3config.ParseDSN("s3://")
	assert.Expect(err).To(HaveOccurred())
	assert.Expect(err.Error()).To(ContainSubstring("missing bucket"))
}

func TestParseDSN_FullParams(t *testing.T) {
	assert := NewGomegaWithT(t)

	cfg, err := s3config.ParseDSN("s3://ci-bucket/prod/prefix?region=eu-west-1&endpoint=http://minio:9000&sse=AES256&ttl=48h")
	assert.Expect(err).NotTo(HaveOccurred())
	assert.Expect(cfg.Bucket).To(Equal("ci-bucket"))
	assert.Expect(cfg.Prefix).To(Equal("prod/prefix"))
	assert.Expect(cfg.Region).To(Equal("eu-west-1"))
	assert.Expect(cfg.Endpoint).To(Equal("http://minio:9000"))
	assert.Expect(cfg.ForcePathStyle).To(BeTrue())
	assert.Expect(cfg.SSE).To(Equal(types.ServerSideEncryptionAes256))
	assert.Expect(cfg.TTL).To(Equal(48 * time.Hour))
}

func TestClientOptions_Empty(t *testing.T) {
	assert := NewGomegaWithT(t)

	cfg, _ := s3config.ParseDSN("s3://mybucket")
	opts := cfg.ClientOptions()
	assert.Expect(opts).To(BeEmpty())
}

func TestClientOptions_WithRegionAndEndpoint(t *testing.T) {
	assert := NewGomegaWithT(t)

	cfg, _ := s3config.ParseDSN("s3://mybucket?region=us-east-1&endpoint=http://localhost:9000")
	opts := cfg.ClientOptions()
	assert.Expect(opts).To(HaveLen(2))
}

func TestParseDSN_InlineCredentials(t *testing.T) {
	assert := NewGomegaWithT(t)

	cfg, err := s3config.ParseDSN("s3://AKID:SECRET@mybucket?region=auto&endpoint=https://account.r2.cloudflarestorage.com")
	assert.Expect(err).NotTo(HaveOccurred())
	assert.Expect(cfg.Bucket).To(Equal("mybucket"))
	assert.Expect(cfg.AccessKeyID).To(Equal("AKID"))
	assert.Expect(cfg.SecretAccessKey).To(Equal("SECRET"))
	assert.Expect(cfg.Region).To(Equal("auto"))
}

func TestParseDSN_NoCredentials(t *testing.T) {
	assert := NewGomegaWithT(t)

	// Without userinfo the credential fields are empty (SDK chain takes over).
	cfg, err := s3config.ParseDSN("s3://mybucket?region=us-east-1")
	assert.Expect(err).NotTo(HaveOccurred())
	assert.Expect(cfg.AccessKeyID).To(Equal(""))
	assert.Expect(cfg.SecretAccessKey).To(Equal(""))
}

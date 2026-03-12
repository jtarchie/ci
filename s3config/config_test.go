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

	cfg, err := s3config.ParseDSN("s3://s3.amazonaws.com/mybucket")
	assert.Expect(err).NotTo(HaveOccurred())
	assert.Expect(cfg.Bucket).To(Equal("mybucket"))
	assert.Expect(cfg.Prefix).To(Equal(""))
	assert.Expect(cfg.Region).To(Equal(""))
	assert.Expect(cfg.Endpoint).To(Equal("https://s3.amazonaws.com"))
	assert.Expect(cfg.ForcePathStyle).To(BeTrue())
	assert.Expect(cfg.SSE).To(BeEmpty())
	assert.Expect(cfg.SSEKMSKeyID).To(Equal(""))
	assert.Expect(cfg.TTL).To(Equal(time.Duration(0)))
}

func TestParseDSN_WithPrefix(t *testing.T) {
	assert := NewGomegaWithT(t)

	cfg, err := s3config.ParseDSN("s3://s3.amazonaws.com/mybucket/some/prefix")
	assert.Expect(err).NotTo(HaveOccurred())
	assert.Expect(cfg.Bucket).To(Equal("mybucket"))
	assert.Expect(cfg.Prefix).To(Equal("some/prefix"))
}

func TestParseDSN_WithRegion(t *testing.T) {
	assert := NewGomegaWithT(t)

	cfg, err := s3config.ParseDSN("s3://s3.amazonaws.com/mybucket?region=us-west-2")
	assert.Expect(err).NotTo(HaveOccurred())
	assert.Expect(cfg.Region).To(Equal("us-west-2"))
}

func TestParseDSN_WithEndpoint_AutoPathStyle(t *testing.T) {
	assert := NewGomegaWithT(t)

	cfg, err := s3config.ParseDSN("s3://http://localhost:9000/mybucket?region=us-east-1")
	assert.Expect(err).NotTo(HaveOccurred())
	assert.Expect(cfg.Bucket).To(Equal("mybucket"))
	assert.Expect(cfg.Endpoint).To(Equal("http://localhost:9000"))
	// ForcePathStyle automatically true when endpoint is set
	assert.Expect(cfg.ForcePathStyle).To(BeTrue())
}

func TestParseDSN_ForcePathStyleFalse(t *testing.T) {
	assert := NewGomegaWithT(t)

	// R2 or providers that require virtual-hosted style
	cfg, err := s3config.ParseDSN("s3://https://account.r2.cloudflarestorage.com/mybucket?force_path_style=false")
	assert.Expect(err).NotTo(HaveOccurred())
	assert.Expect(cfg.Endpoint).NotTo(BeEmpty())
	assert.Expect(cfg.ForcePathStyle).To(BeFalse())
}

func TestParseDSN_SSE_AES256(t *testing.T) {
	assert := NewGomegaWithT(t)

	cfg, err := s3config.ParseDSN("s3://s3.amazonaws.com/mybucket?sse=AES256")
	assert.Expect(err).NotTo(HaveOccurred())
	assert.Expect(cfg.SSE).To(Equal(types.ServerSideEncryptionAes256))
	assert.Expect(cfg.SSEKMSKeyID).To(Equal(""))
}

func TestParseDSN_SSE_KMS_WithoutKey(t *testing.T) {
	assert := NewGomegaWithT(t)

	// Allow aws:kms without a key ID — provider uses its default KMS key.
	cfg, err := s3config.ParseDSN("s3://s3.amazonaws.com/mybucket?sse=aws:kms")
	assert.Expect(err).NotTo(HaveOccurred())
	assert.Expect(cfg.SSE).To(Equal(types.ServerSideEncryptionAwsKms))
	assert.Expect(cfg.SSEKMSKeyID).To(Equal(""))
}

func TestParseDSN_SSE_KMS_WithKey(t *testing.T) {
	assert := NewGomegaWithT(t)

	cfg, err := s3config.ParseDSN("s3://s3.amazonaws.com/mybucket?sse=aws:kms&sse_kms_key_id=arn:aws:kms:us-east-1:123456789012:key/mrk-abc123")
	assert.Expect(err).NotTo(HaveOccurred())
	assert.Expect(cfg.SSE).To(Equal(types.ServerSideEncryptionAwsKms))
	assert.Expect(cfg.SSEKMSKeyID).To(Equal("arn:aws:kms:us-east-1:123456789012:key/mrk-abc123"))
}

func TestParseDSN_SSE_InvalidValue(t *testing.T) {
	assert := NewGomegaWithT(t)

	_, err := s3config.ParseDSN("s3://s3.amazonaws.com/mybucket?sse=SSE-C")
	assert.Expect(err).To(HaveOccurred())
	assert.Expect(err.Error()).To(ContainSubstring("unsupported sse value"))
}

func TestParseDSN_TTL(t *testing.T) {
	assert := NewGomegaWithT(t)

	cfg, err := s3config.ParseDSN("s3://s3.amazonaws.com/mybucket?ttl=24h")
	assert.Expect(err).NotTo(HaveOccurred())
	assert.Expect(cfg.TTL).To(Equal(24 * time.Hour))
}

func TestParseDSN_TTL_Invalid(t *testing.T) {
	assert := NewGomegaWithT(t)

	_, err := s3config.ParseDSN("s3://s3.amazonaws.com/mybucket?ttl=notaduration")
	assert.Expect(err).To(HaveOccurred())
	assert.Expect(err.Error()).To(ContainSubstring("failed to parse ttl"))
}

func TestParseDSN_WrongScheme(t *testing.T) {
	assert := NewGomegaWithT(t)

	_, err := s3config.ParseDSN("https://s3.amazonaws.com/mybucket")
	assert.Expect(err).To(HaveOccurred())
	assert.Expect(err.Error()).To(ContainSubstring("expected s3://"))
}

func TestParseDSN_MissingBucket(t *testing.T) {
	assert := NewGomegaWithT(t)

	_, err := s3config.ParseDSN("s3://http://localhost:9000")
	assert.Expect(err).To(HaveOccurred())
	assert.Expect(err.Error()).To(ContainSubstring("missing bucket"))
}

func TestParseDSN_FullParams(t *testing.T) {
	assert := NewGomegaWithT(t)

	cfg, err := s3config.ParseDSN("s3://http://minio:9000/ci-bucket/prod/prefix?region=eu-west-1&sse=AES256&ttl=48h")
	assert.Expect(err).NotTo(HaveOccurred())
	assert.Expect(cfg.Bucket).To(Equal("ci-bucket"))
	assert.Expect(cfg.Prefix).To(Equal("prod/prefix"))
	assert.Expect(cfg.Region).To(Equal("eu-west-1"))
	assert.Expect(cfg.Endpoint).To(Equal("http://minio:9000"))
	assert.Expect(cfg.ForcePathStyle).To(BeTrue())
	assert.Expect(cfg.SSE).To(Equal(types.ServerSideEncryptionAes256))
	assert.Expect(cfg.TTL).To(Equal(48 * time.Hour))
}

func TestClientOptions_NoCustomEndpoint(t *testing.T) {
	assert := NewGomegaWithT(t)

	// When using standard AWS endpoint, ClientOptions still sets endpoint
	// to the parsed host (s3.amazonaws.com); only region is a separate option.
	cfg, _ := s3config.ParseDSN("s3://s3.amazonaws.com/mybucket")
	opts := cfg.ClientOptions()
	// endpoint + path-style = 1 option (region is empty so no region option)
	assert.Expect(opts).To(HaveLen(1))
}

func TestClientOptions_WithRegionAndEndpoint(t *testing.T) {
	assert := NewGomegaWithT(t)

	cfg, _ := s3config.ParseDSN("s3://http://localhost:9000/mybucket?region=us-east-1")
	opts := cfg.ClientOptions()
	assert.Expect(opts).To(HaveLen(2))
}

func TestParseDSN_InlineCredentials(t *testing.T) {
	assert := NewGomegaWithT(t)

	cfg, err := s3config.ParseDSN("s3://https://AKID:SECRET@account.r2.cloudflarestorage.com/mybucket?region=auto")
	assert.Expect(err).NotTo(HaveOccurred())
	assert.Expect(cfg.Bucket).To(Equal("mybucket"))
	assert.Expect(cfg.Endpoint).To(Equal("https://account.r2.cloudflarestorage.com"))
	assert.Expect(cfg.AccessKeyID).To(Equal("AKID"))
	assert.Expect(cfg.SecretAccessKey).To(Equal("SECRET"))
	assert.Expect(cfg.Region).To(Equal("auto"))
}

func TestParseDSN_NoCredentials(t *testing.T) {
	assert := NewGomegaWithT(t)

	// Without userinfo the credential fields are empty (SDK chain takes over).
	cfg, err := s3config.ParseDSN("s3://s3.amazonaws.com/mybucket?region=us-east-1")
	assert.Expect(err).NotTo(HaveOccurred())
	assert.Expect(cfg.AccessKeyID).To(Equal(""))
	assert.Expect(cfg.SecretAccessKey).To(Equal(""))
}

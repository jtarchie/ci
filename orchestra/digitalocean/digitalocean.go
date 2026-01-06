package digitalocean

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/digitalocean/godo"
	"github.com/jtarchie/ci/orchestra"
	"github.com/jtarchie/ci/orchestra/docker"
	"golang.org/x/crypto/ssh"
)

// Default values.
const (
	DefaultImage         = "docker-20-04"
	DefaultSize          = "s-1vcpu-1gb"
	DefaultRegion        = "nyc3"
	DefaultDiskSizeGB    = 25              // Default disk size in GB
	DefaultSSHTimeout    = 5 * time.Minute // Default timeout for SSH to become available
	DefaultDockerTimeout = 5 * time.Minute // Default timeout for Docker to become available
)

// sanitizeHostname converts a string to a valid hostname.
// DigitalOcean hostnames only allow: a-z, A-Z, 0-9, . and -
func sanitizeHostname(name string) string {
	var result strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
			result.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			result.WriteRune(r)
		case r >= '0' && r <= '9':
			result.WriteRune(r)
		case r == '.' || r == '-':
			result.WriteRune(r)
		default:
			// Replace invalid characters with hyphen
			result.WriteRune('-')
		}
	}
	return result.String()
}

// DigitalOcean implements orchestra.Driver by creating a DigitalOcean droplet
// that runs Docker and delegates container operations to the docker driver.
type DigitalOcean struct {
	client     *godo.Client
	logger     *slog.Logger
	namespace  string
	params     map[string]string
	droplet    *godo.Droplet
	sshKeyID   int
	sshKeyPath string

	// SSH connection to the droplet for Docker communication
	sshClient *ssh.Client

	// Underlying docker driver connected to the droplet
	dockerDriver orchestra.Driver
}

// NewDigitalOcean creates a new Digital Ocean driver instance.
// DSN parameters:
// - token: Digital Ocean API token (required, or DIGITALOCEAN_TOKEN env var)
// - image: Droplet image slug (default: docker-20-04)
// - size: Droplet size slug or "auto" (default: s-1vcpu-1gb)
// - region: Droplet region (default: nyc3)
// - disk_size: Volume disk size in GB (default: 25)
// - tags: Comma-separated list of custom tags to apply to resources
func NewDigitalOcean(namespace string, logger *slog.Logger, params map[string]string) (orchestra.Driver, error) {
	token := orchestra.GetParam(params, "token", "DIGITALOCEAN_TOKEN", "")
	if token == "" {
		return nil, fmt.Errorf("digitalocean: API token is required (set via DSN 'token' param or DIGITALOCEAN_TOKEN env var)")
	}

	client := godo.NewFromToken(token)

	return &DigitalOcean{
		client:    client,
		logger:    logger,
		namespace: namespace,
		params:    params,
	}, nil
}

func (d *DigitalOcean) Name() string {
	return "digitalocean"
}

// ensureDroplet creates a droplet if one doesn't exist for this driver instance.
func (d *DigitalOcean) ensureDroplet(ctx context.Context, containerLimits orchestra.ContainerLimits) error {
	if d.droplet != nil && d.dockerDriver != nil {
		return nil
	}

	d.logger.Info("digitalocean.droplet.creating")

	// Generate SSH key for this session
	sshKeyID, sshKeyPath, err := d.ensureSSHKey(ctx)
	if err != nil {
		return fmt.Errorf("failed to ensure SSH key: %w", err)
	}

	d.sshKeyID = sshKeyID
	d.sshKeyPath = sshKeyPath

	image := orchestra.GetParam(d.params, "image", "DIGITALOCEAN_IMAGE", DefaultImage)
	region := orchestra.GetParam(d.params, "region", "DIGITALOCEAN_REGION", DefaultRegion)
	size := d.determineDropletSize(containerLimits)

	dropletName := fmt.Sprintf("ci-%s", sanitizeHostname(d.namespace))

	// Build tags list: always include ci and namespace, plus any custom tags
	tags := []string{
		"ci",
		fmt.Sprintf("namespace-%s", sanitizeHostname(d.namespace)),
	}

	// Add custom tags from DSN parameter
	customTags := orchestra.GetParam(d.params, "tags", "DIGITALOCEAN_TAGS", "")
	if customTags != "" {
		for _, tag := range strings.Split(customTags, ",") {
			tag = strings.TrimSpace(tag)
			if tag != "" {
				tags = append(tags, sanitizeHostname(tag))
			}
		}
	}

	createRequest := &godo.DropletCreateRequest{
		Name:   dropletName,
		Region: region,
		Size:   size,
		Image: godo.DropletCreateImage{
			Slug: image,
		},
		SSHKeys: []godo.DropletCreateSSHKey{
			{ID: sshKeyID},
		},
		Tags: tags,
	}

	d.logger.Debug("digitalocean.droplet.create_request",
		"name", dropletName,
		"region", region,
		"size", size,
		"image", image,
	)

	droplet, _, err := d.client.Droplets.Create(ctx, createRequest)
	if err != nil {
		return fmt.Errorf("failed to create droplet: %w", err)
	}

	// Store droplet immediately so Close() can clean it up even if subsequent steps fail
	d.droplet = droplet

	d.logger.Info("digitalocean.droplet.created", "id", droplet.ID, "name", dropletName)

	// Wait for droplet to become active and get its public IP
	droplet, err = d.waitForDroplet(ctx, droplet.ID)
	if err != nil {
		return fmt.Errorf("failed to wait for droplet: %w", err)
	}

	d.droplet = droplet

	// Get droplet's public IP
	publicIP, err := droplet.PublicIPv4()
	if err != nil {
		return fmt.Errorf("failed to get droplet public IP: %w", err)
	}

	d.logger.Info("digitalocean.droplet.ready", "ip", publicIP)

	// Wait for SSH to be available (also stores d.sshClient)
	if err := d.waitForSSH(ctx, publicIP); err != nil {
		return fmt.Errorf("failed to wait for SSH: %w", err)
	}

	// Wait for Docker to be ready (uses the SSH client established in waitForSSH)
	if err := d.waitForDocker(ctx); err != nil {
		return fmt.Errorf("failed to wait for Docker: %w", err)
	}

	// Create docker driver connected to the droplet via Go's SSH library
	d.logger.Info("digitalocean.docker.connecting", "ip", publicIP)

	dockerDriver, err := docker.NewDockerWithSSH(d.namespace, d.logger, d.sshClient)
	if err != nil {
		return fmt.Errorf("failed to create docker driver: %w", err)
	}

	d.dockerDriver = dockerDriver
	d.logger.Info("digitalocean.docker.connected")

	return nil
}

// determineDropletSize selects an appropriate droplet size based on container limits.
func (d *DigitalOcean) determineDropletSize(limits orchestra.ContainerLimits) string {
	sizeParam := orchestra.GetParam(d.params, "size", "DIGITALOCEAN_SIZE", DefaultSize)

	if sizeParam != "auto" {
		return sizeParam
	}

	// Auto-determine size based on container limits
	// Digital Ocean droplet sizes:
	// s-1vcpu-1gb:    1 vCPU, 1GB RAM
	// s-1vcpu-2gb:    1 vCPU, 2GB RAM
	// s-2vcpu-2gb:    2 vCPU, 2GB RAM
	// s-2vcpu-4gb:    2 vCPU, 4GB RAM
	// s-4vcpu-8gb:    4 vCPU, 8GB RAM
	// s-8vcpu-16gb:   8 vCPU, 16GB RAM

	memoryMB := limits.Memory / (1024 * 1024) // Convert bytes to MB
	cpuShares := limits.CPU

	d.logger.Debug("digitalocean.size.auto",
		"memory_mb", memoryMB,
		"cpu_shares", cpuShares,
	)

	// Map container limits to droplet sizes
	// CPU shares in Docker: 1024 shares = 1 CPU core (roughly)
	// Memory is more straightforward

	switch {
	case memoryMB > 8192 || cpuShares > 4096:
		return "s-8vcpu-16gb"
	case memoryMB > 4096 || cpuShares > 2048:
		return "s-4vcpu-8gb"
	case memoryMB > 2048 || cpuShares > 1024:
		return "s-2vcpu-4gb"
	case memoryMB > 1024:
		return "s-2vcpu-2gb"
	case memoryMB > 512:
		return "s-1vcpu-2gb"
	default:
		return DefaultSize
	}
}

// ensureSSHKey creates or retrieves an SSH key for droplet access.
func (d *DigitalOcean) ensureSSHKey(ctx context.Context) (int, string, error) {
	keyName := fmt.Sprintf("ci-%s", sanitizeHostname(d.namespace))

	// Check if SSH key already exists in DO
	keys, _, err := d.client.Keys.List(ctx, &godo.ListOptions{})
	if err != nil {
		return 0, "", fmt.Errorf("failed to list SSH keys: %w", err)
	}

	for _, key := range keys {
		if key.Name == keyName {
			d.logger.Debug("digitalocean.ssh_key.exists", "name", keyName, "id", key.ID)

			// Try to find the local key file
			sshKeyPath := filepath.Join(os.TempDir(), fmt.Sprintf("ci-do-%s", d.namespace))
			if _, err := os.Stat(sshKeyPath); err == nil {
				return key.ID, sshKeyPath, nil
			}

			// Key exists in DO but not locally, delete and recreate
			_, err = d.client.Keys.DeleteByID(ctx, key.ID)
			if err != nil {
				d.logger.Warn("digitalocean.ssh_key.delete_failed", "err", err)
			}

			break
		}
	}

	// Generate new SSH key pair
	privateKey, err := rsa.GenerateKey(rand.Reader, 4096)
	if err != nil {
		return 0, "", fmt.Errorf("failed to generate SSH key: %w", err)
	}

	// Save private key to temp file
	sshKeyPath := filepath.Join(os.TempDir(), fmt.Sprintf("ci-do-%s", d.namespace))
	privateKeyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(privateKey),
	})

	if err := os.WriteFile(sshKeyPath, privateKeyPEM, 0o600); err != nil {
		return 0, "", fmt.Errorf("failed to write SSH private key: %w", err)
	}

	// Generate public key
	publicKey, err := ssh.NewPublicKey(&privateKey.PublicKey)
	if err != nil {
		return 0, "", fmt.Errorf("failed to generate SSH public key: %w", err)
	}

	publicKeyStr := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(publicKey)))

	// Create key in Digital Ocean
	createRequest := &godo.KeyCreateRequest{
		Name:      keyName,
		PublicKey: publicKeyStr,
	}

	key, _, err := d.client.Keys.Create(ctx, createRequest)
	if err != nil {
		return 0, "", fmt.Errorf("failed to create SSH key in DO: %w", err)
	}

	d.logger.Info("digitalocean.ssh_key.created", "name", keyName, "id", key.ID)

	return key.ID, sshKeyPath, nil
}

// waitForDroplet polls until the droplet is active.
func (d *DigitalOcean) waitForDroplet(ctx context.Context, dropletID int) (*godo.Droplet, error) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	timeout := time.After(5 * time.Minute)

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timeout:
			return nil, fmt.Errorf("timeout waiting for droplet to become active")
		case <-ticker.C:
			droplet, _, err := d.client.Droplets.Get(ctx, dropletID)
			if err != nil {
				d.logger.Warn("digitalocean.droplet.poll_error", "err", err)

				continue
			}

			d.logger.Debug("digitalocean.droplet.status", "status", droplet.Status)

			if droplet.Status == "active" {
				return droplet, nil
			}
		}
	}
}

// waitForSSH polls until SSH is accessible on the droplet.
func (d *DigitalOcean) waitForSSH(ctx context.Context, ip string) error {
	d.logger.Info("digitalocean.ssh.waiting", "ip", ip)

	// Get configurable timeout
	sshTimeoutStr := orchestra.GetParam(d.params, "ssh_timeout", "DIGITALOCEAN_SSH_TIMEOUT", "")
	sshTimeout := DefaultSSHTimeout
	if sshTimeoutStr != "" {
		if parsed, err := time.ParseDuration(sshTimeoutStr); err == nil {
			sshTimeout = parsed
		}
	}

	// Load private key
	privateKeyData, err := os.ReadFile(d.sshKeyPath)
	if err != nil {
		return fmt.Errorf("failed to read SSH private key: %w", err)
	}

	signer, err := ssh.ParsePrivateKey(privateKeyData)
	if err != nil {
		return fmt.Errorf("failed to parse SSH private key: %w", err)
	}

	config := &ssh.ClientConfig{
		User: "root",
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(signer),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), //nolint:gosec // CI droplets are ephemeral
		Timeout:         10 * time.Second,
	}

	deadline := time.Now().Add(sshTimeout)

	// Try immediately first, then poll
	for {
		if time.Now().After(deadline) {
			return fmt.Errorf("timeout waiting for SSH after %s", sshTimeout)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		conn, err := ssh.Dial("tcp", ip+":22", config)
		if err != nil {
			d.logger.Debug("digitalocean.ssh.connecting", "ip", ip, "err", err)
			time.Sleep(5 * time.Second)

			continue
		}

		// Store the connection for reuse by waitForDocker
		d.sshClient = conn
		d.logger.Info("digitalocean.ssh.connected")

		return nil
	}
}

// waitForDocker polls until Docker is accessible on the droplet.
// Uses the existing SSH client connection established in waitForSSH.
func (d *DigitalOcean) waitForDocker(ctx context.Context) error {
	d.logger.Info("digitalocean.docker.waiting")

	// Get configurable timeout
	dockerTimeoutStr := orchestra.GetParam(d.params, "docker_timeout", "DIGITALOCEAN_DOCKER_TIMEOUT", "")
	dockerTimeout := DefaultDockerTimeout
	if dockerTimeoutStr != "" {
		if parsed, err := time.ParseDuration(dockerTimeoutStr); err == nil {
			dockerTimeout = parsed
		}
	}

	if d.sshClient == nil {
		return fmt.Errorf("SSH client not connected")
	}

	deadline := time.Now().Add(dockerTimeout)

	// Try immediately first, then poll
	for {
		if time.Now().After(deadline) {
			return fmt.Errorf("timeout waiting for Docker after %s", dockerTimeout)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		session, err := d.sshClient.NewSession()
		if err != nil {
			d.logger.Debug("digitalocean.docker.session_error", "err", err)
			time.Sleep(5 * time.Second)

			continue
		}

		output, err := session.CombinedOutput("docker ps")
		_ = session.Close()

		if err != nil {
			d.logger.Debug("digitalocean.docker.check_error", "err", err, "output", string(output))
			time.Sleep(5 * time.Second)

			continue
		}

		d.logger.Info("digitalocean.docker.ready")

		return nil
	}
}

// RunContainer creates the droplet if needed and delegates to the docker driver.
func (d *DigitalOcean) RunContainer(ctx context.Context, task orchestra.Task) (orchestra.Container, error) {
	if err := d.ensureDroplet(ctx, task.ContainerLimits); err != nil {
		return nil, fmt.Errorf("failed to ensure droplet: %w", err)
	}

	return d.dockerDriver.RunContainer(ctx, task)
}

// GetContainer attempts to find and return an existing container by its ID.
// Delegates to the docker driver after ensuring the droplet exists.
func (d *DigitalOcean) GetContainer(ctx context.Context, containerID string) (orchestra.Container, error) {
	if d.dockerDriver == nil {
		return nil, orchestra.ErrContainerNotFound
	}
	return d.dockerDriver.GetContainer(ctx, containerID)
}

// CreateVolume creates a volume on the droplet's docker instance.
// The size parameter is used when creating Digital Ocean block storage volumes.
func (d *DigitalOcean) CreateVolume(ctx context.Context, name string, size int) (orchestra.Volume, error) {
	if err := d.ensureDroplet(ctx, orchestra.ContainerLimits{}); err != nil {
		return nil, fmt.Errorf("failed to ensure droplet: %w", err)
	}

	// Get disk size from params if not specified
	if size <= 0 {
		diskSizeStr := orchestra.GetParam(d.params, "disk_size", "DIGITALOCEAN_DISK_SIZE", strconv.Itoa(DefaultDiskSizeGB))

		parsedSize, err := strconv.Atoi(diskSizeStr)
		if err != nil {
			d.logger.Warn("digitalocean.volume.invalid_disk_size", "value", diskSizeStr, "err", err)
			parsedSize = DefaultDiskSizeGB
		}

		size = parsedSize
	}

	// For now, delegate to docker driver's volume creation
	// In the future, we could create DO block storage volumes for larger needs
	return d.dockerDriver.CreateVolume(ctx, name, size)
}

// Close deletes the droplet and cleans up resources.
func (d *DigitalOcean) Close() error {
	ctx := context.Background()

	// Close docker driver first
	if d.dockerDriver != nil {
		if err := d.dockerDriver.Close(); err != nil {
			d.logger.Warn("digitalocean.docker.close_error", "err", err)
		}
	}

	// Close SSH client
	if d.sshClient != nil {
		if err := d.sshClient.Close(); err != nil {
			d.logger.Warn("digitalocean.ssh.close_error", "err", err)
		}
	}

	// Delete droplet
	if d.droplet != nil {
		d.logger.Info("digitalocean.droplet.deleting", "id", d.droplet.ID)

		_, err := d.client.Droplets.Delete(ctx, d.droplet.ID)
		if err != nil {
			d.logger.Error("digitalocean.droplet.delete_error", "err", err)

			return fmt.Errorf("failed to delete droplet: %w", err)
		}

		d.logger.Info("digitalocean.droplet.deleted", "id", d.droplet.ID)
	}

	// Delete SSH key from Digital Ocean
	if d.sshKeyID != 0 {
		_, err := d.client.Keys.DeleteByID(ctx, d.sshKeyID)
		if err != nil {
			d.logger.Warn("digitalocean.ssh_key.delete_error", "err", err)
		}
	}

	// Delete local SSH key file
	if d.sshKeyPath != "" {
		if err := os.Remove(d.sshKeyPath); err != nil && !os.IsNotExist(err) {
			d.logger.Warn("digitalocean.ssh_key.local_delete_error", "err", err)
		}
	}

	return nil
}

// CleanupOrphanedResources deletes droplets and SSH keys matching the specified tag.
// If tag is empty, it defaults to "ci" which matches all CI-created resources.
// For more targeted cleanup, use a specific tag like "ci-test" or a namespace tag.
// This is useful for cleaning up resources from failed or interrupted runs.
func CleanupOrphanedResources(ctx context.Context, token string, logger *slog.Logger, tag string) error {
	if tag == "" {
		tag = "ci"
	}

	client := godo.NewFromToken(token)

	// List all droplets with the specified tag
	droplets, _, err := client.Droplets.ListByTag(ctx, tag, &godo.ListOptions{PerPage: 200})
	if err != nil {
		return fmt.Errorf("failed to list droplets: %w", err)
	}

	for _, droplet := range droplets {
		logger.Info("digitalocean.cleanup.deleting_droplet", "id", droplet.ID, "name", droplet.Name, "tag", tag)

		_, err := client.Droplets.Delete(ctx, droplet.ID)
		if err != nil {
			logger.Warn("digitalocean.cleanup.droplet_delete_error", "id", droplet.ID, "err", err)
		} else {
			logger.Info("digitalocean.cleanup.droplet_deleted", "id", droplet.ID)
		}
	}

	// List all SSH keys and delete those matching the tag pattern
	// SSH keys are named "ci-<namespace>" so we look for keys starting with the tag
	keyPrefix := tag + "-"
	if tag == "ci" {
		keyPrefix = "ci-"
	}

	keys, _, err := client.Keys.List(ctx, &godo.ListOptions{PerPage: 200})
	if err != nil {
		return fmt.Errorf("failed to list SSH keys: %w", err)
	}

	for _, key := range keys {
		if strings.HasPrefix(key.Name, keyPrefix) {
			logger.Info("digitalocean.cleanup.deleting_ssh_key", "id", key.ID, "name", key.Name)

			_, err := client.Keys.DeleteByID(ctx, key.ID)
			if err != nil {
				logger.Warn("digitalocean.cleanup.ssh_key_delete_error", "id", key.ID, "err", err)
			} else {
				logger.Info("digitalocean.cleanup.ssh_key_deleted", "id", key.ID)
			}
		}
	}

	return nil
}

func init() {
	orchestra.Add("digitalocean", NewDigitalOcean)
}

var _ orchestra.Driver = &DigitalOcean{}

package hetzner

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

	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"github.com/jtarchie/ci/orchestra"
	"github.com/jtarchie/ci/orchestra/docker"
	"golang.org/x/crypto/ssh"
)

// Default values.
const (
	DefaultImage         = "docker-ce" // Hetzner app image with Docker pre-installed
	DefaultServerType    = "cx23"      // 2 vCPU, 4GB RAM (smallest shared vCPU)
	DefaultLocation      = "nbg1"      // Nuremberg, Germany
	DefaultSSHTimeout    = 5 * time.Minute
	DefaultDockerTimeout = 5 * time.Minute
)

// sanitizeHostname converts a string to a valid hostname.
// Hetzner server names only allow: a-z, A-Z, 0-9, and -
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
		case r == '-':
			result.WriteRune(r)
		default:
			// Replace invalid characters with hyphen
			result.WriteRune('-')
		}
	}

	return result.String()
}

// Hetzner implements orchestra.Driver by creating a Hetzner Cloud server
// that runs Docker and delegates container operations to the docker driver.
type Hetzner struct {
	client     *hcloud.Client
	logger     *slog.Logger
	namespace  string
	params     map[string]string
	server     *hcloud.Server
	sshKey     *hcloud.SSHKey
	sshKeyPath string

	// SSH connection to the server for Docker communication
	sshClient *ssh.Client

	// Underlying docker driver connected to the server
	dockerDriver orchestra.Driver
}

// NewHetzner creates a new Hetzner Cloud driver instance.
// DSN parameters:
// - token: Hetzner Cloud API token (required, or HETZNER_TOKEN env var)
// - image: Server image name (default: docker-ce)
// - server_type: Server type or "auto" (default: cx23)
// - location: Server location (default: nbg1)
// - ssh_timeout: Timeout for SSH to become available (default: 5m)
// - docker_timeout: Timeout for Docker to become available (default: 5m)
// - labels: Comma-separated list of key=value labels to apply to resources
func NewHetzner(namespace string, logger *slog.Logger, params map[string]string) (orchestra.Driver, error) {
	token := orchestra.GetParam(params, "token", "HETZNER_TOKEN", "")
	if token == "" {
		return nil, fmt.Errorf("hetzner: API token is required (set via DSN 'token' param or HETZNER_TOKEN env var)")
	}

	client := hcloud.NewClient(hcloud.WithToken(token))

	// Sanitize namespace to ensure it contains only valid hostname characters
	// This is required because the namespace is used in server names, container names,
	// volume names, and other resources that have hostname restrictions
	sanitizedNamespace := sanitizeHostname(namespace)

	return &Hetzner{
		client:    client,
		logger:    logger,
		namespace: sanitizedNamespace,
		params:    params,
	}, nil
}

func (h *Hetzner) Name() string {
	return "hetzner"
}

// ensureServer creates a server if one doesn't exist for this driver instance.
func (h *Hetzner) ensureServer(ctx context.Context, containerLimits orchestra.ContainerLimits) error {
	if h.server != nil && h.dockerDriver != nil {
		return nil
	}

	h.logger.Info("hetzner.server.creating")

	// Generate SSH key for this session
	sshKey, sshKeyPath, err := h.ensureSSHKey(ctx)
	if err != nil {
		return fmt.Errorf("failed to ensure SSH key: %w", err)
	}

	h.sshKey = sshKey
	h.sshKeyPath = sshKeyPath

	image := orchestra.GetParam(h.params, "image", "HETZNER_IMAGE", DefaultImage)
	location := orchestra.GetParam(h.params, "location", "HETZNER_LOCATION", DefaultLocation)
	serverType := h.determineServerType(containerLimits)

	serverName := fmt.Sprintf("ci-%s", h.namespace)

	// Look up the image
	imageResult, _, err := h.client.Image.GetByNameAndArchitecture(ctx, image, hcloud.ArchitectureX86)
	if err != nil {
		return fmt.Errorf("failed to get image %s: %w", image, err)
	}

	if imageResult == nil {
		return fmt.Errorf("image %s not found", image)
	}

	// Look up the server type
	serverTypeResult, _, err := h.client.ServerType.GetByName(ctx, serverType)
	if err != nil {
		return fmt.Errorf("failed to get server type %s: %w", serverType, err)
	}

	if serverTypeResult == nil {
		return fmt.Errorf("server type %s not found", serverType)
	}

	// Look up the location
	locationResult, _, err := h.client.Location.GetByName(ctx, location)
	if err != nil {
		return fmt.Errorf("failed to get location %s: %w", location, err)
	}

	if locationResult == nil {
		return fmt.Errorf("location %s not found", location)
	}

	// Build labels map: always include ci and namespace, plus any custom labels
	labels := map[string]string{
		"ci":        "true",
		"namespace": h.namespace,
	}

	// Add custom labels from DSN parameter (format: key1=value1,key2=value2)
	customLabels := orchestra.GetParam(h.params, "labels", "HETZNER_LABELS", "")
	if customLabels != "" {
		for label := range strings.SplitSeq(customLabels, ",") {
			label = strings.TrimSpace(label)
			if parts := strings.SplitN(label, "=", 2); len(parts) == 2 {
				key := sanitizeHostname(strings.TrimSpace(parts[0]))
				value := sanitizeHostname(strings.TrimSpace(parts[1]))
				if key != "" {
					labels[key] = value
				}
			}
		}
	}

	createOpts := hcloud.ServerCreateOpts{
		Name:       serverName,
		ServerType: serverTypeResult,
		Image:      imageResult,
		Location:   locationResult,
		SSHKeys:    []*hcloud.SSHKey{sshKey},
		Labels:     labels,
	}

	h.logger.Debug("hetzner.server.create_request",
		"name", serverName,
		"location", location,
		"server_type", serverType,
		"image", image,
	)

	result, _, err := h.client.Server.Create(ctx, createOpts)
	if err != nil {
		return fmt.Errorf("failed to create server: %w", err)
	}

	// Store server immediately so Close() can clean it up even if subsequent steps fail
	h.server = result.Server

	h.logger.Info("hetzner.server.created", "id", result.Server.ID, "name", serverName)

	// Wait for server to become running
	server, err := h.waitForServer(ctx, result.Server.ID)
	if err != nil {
		return fmt.Errorf("failed to wait for server: %w", err)
	}

	h.server = server

	// Get server's public IP
	publicIP := server.PublicNet.IPv4.IP.String()

	h.logger.Info("hetzner.server.ready", "ip", publicIP)

	// Wait for SSH to be available (also stores h.sshClient)
	if err := h.waitForSSH(ctx, publicIP); err != nil {
		return fmt.Errorf("failed to wait for SSH: %w", err)
	}

	// Wait for Docker to be ready (uses the SSH client established in waitForSSH)
	if err := h.waitForDocker(ctx); err != nil {
		return fmt.Errorf("failed to wait for Docker: %w", err)
	}

	// Create docker driver connected to the server via Go's SSH library
	h.logger.Info("hetzner.docker.connecting", "ip", publicIP)

	dockerDriver, err := docker.NewDockerWithSSH(h.namespace, h.logger, h.sshClient)
	if err != nil {
		return fmt.Errorf("failed to create docker driver: %w", err)
	}

	h.dockerDriver = dockerDriver
	h.logger.Info("hetzner.docker.connected")

	return nil
}

// determineServerType selects an appropriate server type based on container limits.
func (h *Hetzner) determineServerType(limits orchestra.ContainerLimits) string {
	sizeParam := orchestra.GetParam(h.params, "server_type", "HETZNER_SERVER_TYPE", DefaultServerType)

	if sizeParam != "auto" {
		return sizeParam
	}

	// Auto-determine size based on container limits
	// Hetzner shared vCPU server types (CX line):
	// cx23:  2 vCPU, 4GB RAM
	// cx33:  4 vCPU, 8GB RAM
	// cx43:  8 vCPU, 16GB RAM
	// cx53: 16 vCPU, 32GB RAM
	//
	// Hetzner dedicated vCPU server types (CCX line):
	// ccx13:  2 vCPU, 8GB RAM
	// ccx23:  4 vCPU, 16GB RAM
	// ccx33:  8 vCPU, 32GB RAM
	// ccx43: 16 vCPU, 64GB RAM
	// ccx53: 32 vCPU, 128GB RAM
	// ccx63: 48 vCPU, 192GB RAM

	memoryMB := limits.Memory / (1024 * 1024) // Convert bytes to MB
	cpuShares := limits.CPU

	h.logger.Debug("hetzner.size.auto",
		"memory_mb", memoryMB,
		"cpu_shares", cpuShares,
	)

	// Map container limits to server types (using shared vCPU for cost efficiency)
	// CPU shares in Docker: 1024 shares = 1 CPU core (roughly)
	switch {
	case memoryMB > 16384 || cpuShares > 8192:
		return "cx53" // 16 vCPU, 32GB RAM
	case memoryMB > 8192 || cpuShares > 4096:
		return "cx43" // 8 vCPU, 16GB RAM
	case memoryMB > 4096 || cpuShares > 2048:
		return "cx33" // 4 vCPU, 8GB RAM
	default:
		return DefaultServerType // cx23: 2 vCPU, 4GB RAM
	}
}

// ensureSSHKey creates or retrieves an SSH key for server access.
func (h *Hetzner) ensureSSHKey(ctx context.Context) (*hcloud.SSHKey, string, error) {
	keyName := fmt.Sprintf("ci-%s", h.namespace)

	// Check if SSH key already exists in Hetzner
	existingKey, _, err := h.client.SSHKey.GetByName(ctx, keyName)
	if err != nil {
		return nil, "", fmt.Errorf("failed to check for existing SSH key: %w", err)
	}

	if existingKey != nil {
		h.logger.Debug("hetzner.ssh_key.exists", "name", keyName, "id", existingKey.ID)

		// Try to find the local key file
		sshKeyPath := filepath.Join(os.TempDir(), fmt.Sprintf("ci-hetzner-%s", h.namespace))
		if _, err := os.Stat(sshKeyPath); err == nil {
			return existingKey, sshKeyPath, nil
		}

		// Key exists in Hetzner but not locally, delete and recreate
		_, err = h.client.SSHKey.Delete(ctx, existingKey)
		if err != nil {
			h.logger.Warn("hetzner.ssh_key.delete_failed", "err", err)
		}
	}

	// Generate new SSH key pair
	privateKey, err := rsa.GenerateKey(rand.Reader, 4096)
	if err != nil {
		return nil, "", fmt.Errorf("failed to generate SSH key: %w", err)
	}

	// Save private key to temp file
	sshKeyPath := filepath.Join(os.TempDir(), fmt.Sprintf("ci-hetzner-%s", h.namespace))
	privateKeyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(privateKey),
	})

	if err := os.WriteFile(sshKeyPath, privateKeyPEM, 0o600); err != nil {
		return nil, "", fmt.Errorf("failed to write SSH private key: %w", err)
	}

	// Generate public key
	publicKey, err := ssh.NewPublicKey(&privateKey.PublicKey)
	if err != nil {
		return nil, "", fmt.Errorf("failed to generate SSH public key: %w", err)
	}

	publicKeyStr := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(publicKey)))

	// Create key in Hetzner
	createOpts := hcloud.SSHKeyCreateOpts{
		Name:      keyName,
		PublicKey: publicKeyStr,
		Labels: map[string]string{
			"ci": "true",
		},
	}

	key, _, err := h.client.SSHKey.Create(ctx, createOpts)
	if err != nil {
		return nil, "", fmt.Errorf("failed to create SSH key in Hetzner: %w", err)
	}

	h.logger.Info("hetzner.ssh_key.created", "name", keyName, "id", key.ID)

	return key, sshKeyPath, nil
}

// waitForServer polls until the server is running.
func (h *Hetzner) waitForServer(ctx context.Context, serverID int64) (*hcloud.Server, error) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	timeout := time.After(5 * time.Minute)

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timeout:
			return nil, fmt.Errorf("timeout waiting for server to become running")
		case <-ticker.C:
			server, _, err := h.client.Server.GetByID(ctx, serverID)
			if err != nil {
				h.logger.Warn("hetzner.server.poll_error", "err", err)

				continue
			}

			h.logger.Debug("hetzner.server.status", "status", server.Status)

			if server.Status == hcloud.ServerStatusRunning {
				return server, nil
			}
		}
	}
}

// waitForSSH polls until SSH is accessible on the server.
func (h *Hetzner) waitForSSH(ctx context.Context, ip string) error {
	h.logger.Info("hetzner.ssh.waiting", "ip", ip)

	// Get configurable timeout
	sshTimeoutStr := orchestra.GetParam(h.params, "ssh_timeout", "HETZNER_SSH_TIMEOUT", "")
	sshTimeout := DefaultSSHTimeout

	if sshTimeoutStr != "" {
		if parsed, err := time.ParseDuration(sshTimeoutStr); err == nil {
			sshTimeout = parsed
		}
	}

	// Load private key
	privateKeyData, err := os.ReadFile(h.sshKeyPath)
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
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), //nolint:gosec // CI servers are ephemeral
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
			h.logger.Debug("hetzner.ssh.connecting", "ip", ip, "err", err)
			time.Sleep(5 * time.Second)

			continue
		}

		// Store the connection for reuse by waitForDocker
		h.sshClient = conn
		h.logger.Info("hetzner.ssh.connected")

		return nil
	}
}

// waitForDocker polls until Docker is accessible on the server.
// Uses the existing SSH client connection established in waitForSSH.
func (h *Hetzner) waitForDocker(ctx context.Context) error {
	h.logger.Info("hetzner.docker.waiting")

	// Get configurable timeout
	dockerTimeoutStr := orchestra.GetParam(h.params, "docker_timeout", "HETZNER_DOCKER_TIMEOUT", "")
	dockerTimeout := DefaultDockerTimeout

	if dockerTimeoutStr != "" {
		if parsed, err := time.ParseDuration(dockerTimeoutStr); err == nil {
			dockerTimeout = parsed
		}
	}

	if h.sshClient == nil {
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

		session, err := h.sshClient.NewSession()
		if err != nil {
			h.logger.Debug("hetzner.docker.session_error", "err", err)
			time.Sleep(5 * time.Second)

			continue
		}

		output, err := session.CombinedOutput("docker ps")
		_ = session.Close()

		if err != nil {
			h.logger.Debug("hetzner.docker.check_error", "err", err, "output", string(output))
			time.Sleep(5 * time.Second)

			continue
		}

		h.logger.Info("hetzner.docker.ready")

		return nil
	}
}

// RunContainer creates the server if needed and delegates to the docker driver.
func (h *Hetzner) RunContainer(ctx context.Context, task orchestra.Task) (orchestra.Container, error) {
	if err := h.ensureServer(ctx, task.ContainerLimits); err != nil {
		return nil, fmt.Errorf("failed to ensure server: %w", err)
	}

	return h.dockerDriver.RunContainer(ctx, task)
}

// GetContainer attempts to find and return an existing container by its ID.
// Delegates to the docker driver after ensuring the server exists.
func (h *Hetzner) GetContainer(ctx context.Context, containerID string) (orchestra.Container, error) {
	if h.dockerDriver == nil {
		return nil, orchestra.ErrContainerNotFound
	}
	return h.dockerDriver.GetContainer(ctx, containerID)
}

// CreateVolume creates a volume on the server's docker instance.
func (h *Hetzner) CreateVolume(ctx context.Context, name string, size int) (orchestra.Volume, error) {
	if err := h.ensureServer(ctx, orchestra.ContainerLimits{}); err != nil {
		return nil, fmt.Errorf("failed to ensure server: %w", err)
	}

	// Get disk size from params if not specified
	if size <= 0 {
		diskSizeStr := orchestra.GetParam(h.params, "disk_size", "HETZNER_DISK_SIZE", "10")

		parsedSize, err := strconv.Atoi(diskSizeStr)
		if err != nil {
			h.logger.Warn("hetzner.volume.invalid_disk_size", "value", diskSizeStr, "err", err)
			parsedSize = 10
		}

		size = parsedSize
	}

	// For now, delegate to docker driver's volume creation
	// In the future, we could create Hetzner block storage volumes for larger needs
	return h.dockerDriver.CreateVolume(ctx, name, size)
}

// Close deletes the server and cleans up resources.
func (h *Hetzner) Close() error {
	ctx := context.Background()

	// Close docker driver first
	if h.dockerDriver != nil {
		if err := h.dockerDriver.Close(); err != nil {
			h.logger.Warn("hetzner.docker.close_error", "err", err)
		}
	}

	// Close SSH client
	if h.sshClient != nil {
		if err := h.sshClient.Close(); err != nil {
			h.logger.Warn("hetzner.ssh.close_error", "err", err)
		}
	}

	// Delete server
	if h.server != nil {
		h.logger.Info("hetzner.server.deleting", "id", h.server.ID)

		_, _, err := h.client.Server.DeleteWithResult(ctx, h.server)
		if err != nil {
			h.logger.Error("hetzner.server.delete_error", "err", err)

			return fmt.Errorf("failed to delete server: %w", err)
		}

		h.logger.Info("hetzner.server.deleted", "id", h.server.ID)
	}

	// Delete SSH key from Hetzner
	if h.sshKey != nil {
		_, err := h.client.SSHKey.Delete(ctx, h.sshKey)
		if err != nil {
			h.logger.Warn("hetzner.ssh_key.delete_error", "err", err)
		}
	}

	// Delete local SSH key file
	if h.sshKeyPath != "" {
		if err := os.Remove(h.sshKeyPath); err != nil && !os.IsNotExist(err) {
			h.logger.Warn("hetzner.ssh_key.local.delete.error", "err", err)
		}
	}

	return nil
}

// CleanupOrphanedResources deletes servers and SSH keys matching the specified label selector.
// If labelSelector is empty, it defaults to "ci=true" which matches all CI-created resources.
// For more targeted cleanup, use a specific selector like "environment=test" or "namespace=myns".
// This is useful for cleaning up resources from failed or interrupted runs.
func CleanupOrphanedResources(ctx context.Context, token string, logger *slog.Logger, labelSelector string) error {
	if labelSelector == "" {
		labelSelector = "ci=true"
	}

	client := hcloud.NewClient(hcloud.WithToken(token))

	// List all servers with the specified label selector
	servers, err := client.Server.AllWithOpts(ctx, hcloud.ServerListOpts{
		ListOpts: hcloud.ListOpts{
			LabelSelector: labelSelector,
		},
	})
	if err != nil {
		return fmt.Errorf("failed to list servers: %w", err)
	}

	for _, server := range servers {
		logger.Info("hetzner.cleanup.server.deleting", "id", server.ID, "name", server.Name, "selector", labelSelector)

		_, _, err := client.Server.DeleteWithResult(ctx, server)
		if err != nil {
			logger.Warn("hetzner.cleanup.server.delete.error", "id", server.ID, "err", err)
		} else {
			logger.Info("hetzner.cleanup.server.delete.success", "id", server.ID)
		}
	}

	// List all SSH keys and delete those matching the pattern
	// SSH keys are named "ci-<namespace>" so we derive prefix from the label selector
	keyPrefix := "ci-"

	// If selector includes namespace, use it for more targeted cleanup
	if strings.Contains(labelSelector, "namespace=") {
		for part := range strings.SplitSeq(labelSelector, ",") {
			part = strings.TrimSpace(part)
			if after, ok := strings.CutPrefix(part, "namespace="); ok {
				ns := after
				keyPrefix = "ci-" + ns

				break
			}
		}
	}

	keys, err := client.SSHKey.All(ctx)
	if err != nil {
		return fmt.Errorf("failed to list SSH keys: %w", err)
	}

	for _, key := range keys {
		if strings.HasPrefix(key.Name, keyPrefix) {
			logger.Info("hetzner.cleanup.deleting_ssh_key.start", "id", key.ID, "name", key.Name)

			_, err := client.SSHKey.Delete(ctx, key)
			if err != nil {
				logger.Warn("hetzner.cleanup.ssh_key.delete.error", "id", key.ID, "err", err)
			} else {
				logger.Info("hetzner.cleanup.ssh_key_deleted", "id", key.ID)
			}
		}
	}

	return nil
}

func init() {
	orchestra.Add("hetzner", NewHetzner)
}

var _ orchestra.Driver = &Hetzner{}

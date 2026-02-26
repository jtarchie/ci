package fly

import (
	"archive/tar"
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"time"

	"github.com/pkg/sftp"
	fly "github.com/superfly/fly-go"
	"golang.org/x/crypto/ssh"

	"github.com/jtarchie/ci/orchestra/cache"
)

const cacheHelperImage = "busybox:latest"

// findVolumeByName looks up a tracked Fly volume by its user-facing name.
func (f *Fly) findVolumeByName(volumeName string) *Volume {
	f.mu.Lock()
	defer f.mu.Unlock()

	for _, v := range f.volumes {
		if v.userFacingName == volumeName {
			return v
		}
	}

	return nil
}

// launchHelperMachine creates a temporary busybox machine with the given volume
// attached, waits for it to reach "started" state, and returns the machine.
// The caller is responsible for destroying this machine when done.
func (f *Fly) launchHelperMachine(ctx context.Context, vol *Volume) (*fly.Machine, error) {
	// If the volume is currently attached to another machine, detach it first
	f.mu.Lock()
	oldMachineID, attached := f.volumeAttachments[vol.id]
	f.mu.Unlock()

	if attached {
		f.logger.Debug("fly.cache.detach", "volume", vol.id, "oldMachine", oldMachineID)

		err := f.client.Destroy(ctx, f.appName, fly.RemoveMachineInput{
			ID:   oldMachineID,
			Kill: true,
		}, "")
		if err != nil {
			f.logger.Warn("fly.cache.detach.error", "volume", vol.id, "machine", oldMachineID, "err", err)
		}
	}

	guest := &fly.MachineGuest{
		CPUKind:  "shared",
		CPUs:     1,
		MemoryMB: 256,
	}

	config := &fly.MachineConfig{
		Image: cacheHelperImage,
		Init: fly.MachineInit{
			Exec: []string{"sleep", "3600"},
		},
		Guest:       guest,
		AutoDestroy: false,
		Restart: &fly.MachineRestart{
			Policy: fly.MachineRestartPolicyNo,
		},
		Metadata: map[string]string{
			"orchestra.namespace": f.namespace,
			"orchestra.purpose":   "cache-helper",
		},
		Mounts: []fly.MachineMount{
			{
				Volume: vol.id,
				Path:   "/volume",
			},
		},
	}

	input := fly.LaunchMachineInput{
		Config: config,
		Region: f.region,
	}

	f.logger.Debug("fly.cache.helper.launch", "volume", vol.name, "image", cacheHelperImage)

	machine, err := f.client.Launch(ctx, f.appName, input)
	if err != nil {
		return nil, fmt.Errorf("failed to launch cache helper machine: %w", err)
	}

	// Record volume attachment
	f.mu.Lock()
	f.volumeAttachments[vol.id] = machine.ID
	f.mu.Unlock()

	// Wait for the machine to start
	err = f.client.Wait(ctx, f.appName, machine, "started", 2*time.Minute)
	if err != nil {
		// Try to clean up the machine
		_ = f.client.Destroy(ctx, f.appName, fly.RemoveMachineInput{
			ID:   machine.ID,
			Kill: true,
		}, "")

		return nil, fmt.Errorf("cache helper machine failed to start: %w", err)
	}

	// Refresh machine state to get PrivateIP
	machine, err = f.client.Get(ctx, f.appName, machine.ID)
	if err != nil {
		_ = f.client.Destroy(ctx, f.appName, fly.RemoveMachineInput{
			ID:   machine.ID,
			Kill: true,
		}, "")

		return nil, fmt.Errorf("failed to get cache helper machine state: %w", err)
	}

	if machine.PrivateIP == "" {
		_ = f.client.Destroy(ctx, f.appName, fly.RemoveMachineInput{
			ID:   machine.ID,
			Kill: true,
		}, "")

		return nil, fmt.Errorf("cache helper machine has no private IP")
	}

	f.logger.Debug("fly.cache.helper.started", "machine", machine.ID, "ip", machine.PrivateIP)

	return machine, nil
}

// destroyHelperMachine stops and destroys a helper machine.
func (f *Fly) destroyHelperMachine(ctx context.Context, machineID string) {
	f.logger.Debug("fly.cache.helper.destroy", "machine", machineID)

	_ = f.client.Kill(ctx, f.appName, machineID)

	machine := &fly.Machine{ID: machineID}
	_ = f.client.Wait(ctx, f.appName, machine, "stopped", 30*time.Second)

	_ = f.client.Destroy(ctx, f.appName, fly.RemoveMachineInput{
		ID:   machineID,
		Kill: true,
	}, "")
}

// CopyToVolume implements cache.VolumeDataAccessor.
// It launches a busybox helper machine with the volume mounted, establishes a
// WireGuard tunnel + SSH connection, then walks the tar stream on the client
// side and uploads each entry to /volume via SFTP. This requires no tar binary
// on the remote machine.
func (f *Fly) CopyToVolume(ctx context.Context, volumeName string, reader io.Reader) error {
	vol := f.findVolumeByName(volumeName)
	if vol == nil {
		return fmt.Errorf("volume %q not found", volumeName)
	}

	// Launch helper machine with the volume
	machine, err := f.launchHelperMachine(ctx, vol)
	if err != nil {
		return fmt.Errorf("failed to launch helper for CopyToVolume: %w", err)
	}
	defer f.destroyHelperMachine(ctx, machine.ID)

	// Establish WireGuard tunnel
	tunnel, err := f.createTunnel(ctx)
	if err != nil {
		return fmt.Errorf("failed to create WireGuard tunnel: %w", err)
	}
	defer tunnel.close(ctx, f.apiClient)

	// Connect via SSH through the tunnel
	sshClient, err := f.dialSSH(ctx, tunnel, machine.PrivateIP)
	if err != nil {
		return fmt.Errorf("failed to SSH to helper machine: %w", err)
	}
	defer func() { _ = sshClient.Close() }()

	// Open SFTP subsystem over the existing SSH connection.
	sftpClient, err := sftp.NewClient(sshClient,
		sftp.UseConcurrentReads(true),
		sftp.UseConcurrentWrites(true),
	)
	if err != nil {
		return fmt.Errorf("failed to open SFTP subsystem: %w", err)
	}
	defer func() { _ = sftpClient.Close() }()

	f.logger.Debug("fly.cache.copytov.start", "volume", volumeName)

	// Walk the tar stream and upload each entry via SFTP.
	tr := tar.NewReader(reader)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to read tar entry: %w", err)
		}

		remotePath := path.Join("/volume", hdr.Name)

		switch hdr.Typeflag {
		case tar.TypeDir:
			if mkErr := sftpClient.MkdirAll(remotePath); mkErr != nil {
				return fmt.Errorf("failed to create remote directory %q: %w", remotePath, mkErr)
			}

		case tar.TypeReg:
			// Ensure parent directory exists.
			if mkErr := sftpClient.MkdirAll(path.Dir(remotePath)); mkErr != nil {
				return fmt.Errorf("failed to create parent dir for %q: %w", remotePath, mkErr)
			}

			rf, err := sftpClient.OpenFile(remotePath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC)
			if err != nil {
				return fmt.Errorf("failed to open remote file %q: %w", remotePath, err)
			}

			if _, cpErr := io.Copy(rf, tr); cpErr != nil {
				_ = rf.Close()
				return fmt.Errorf("failed to write remote file %q: %w", remotePath, cpErr)
			}

			if closeErr := rf.Close(); closeErr != nil {
				return fmt.Errorf("failed to close remote file %q: %w", remotePath, closeErr)
			}
		}
	}

	f.logger.Info("fly.cache.copytov.done", "volume", volumeName)

	return nil
}

// CopyFromVolume implements cache.VolumeDataAccessor.
// It launches a busybox helper machine with the volume mounted, establishes a
// WireGuard tunnel + SSH connection, and streams a tar archive of /volume contents.
func (f *Fly) CopyFromVolume(ctx context.Context, volumeName string) (io.ReadCloser, error) {
	vol := f.findVolumeByName(volumeName)
	if vol == nil {
		return nil, fmt.Errorf("volume %q not found", volumeName)
	}

	// Launch helper machine with the volume
	machine, err := f.launchHelperMachine(ctx, vol)
	if err != nil {
		return nil, fmt.Errorf("failed to launch helper for CopyFromVolume: %w", err)
	}

	// Establish WireGuard tunnel
	tunnel, err := f.createTunnel(ctx)
	if err != nil {
		f.destroyHelperMachine(ctx, machine.ID)
		return nil, fmt.Errorf("failed to create WireGuard tunnel: %w", err)
	}

	// Connect via SSH through the tunnel
	sshClient, err := f.dialSSH(ctx, tunnel, machine.PrivateIP)
	if err != nil {
		tunnel.close(ctx, f.apiClient)
		f.destroyHelperMachine(ctx, machine.ID)
		return nil, fmt.Errorf("failed to SSH to helper machine: %w", err)
	}

	// Open SSH session and stream tar from /volume
	session, err := sshClient.NewSession()
	if err != nil {
		_ = sshClient.Close()
		tunnel.close(ctx, f.apiClient)
		f.destroyHelperMachine(ctx, machine.ID)
		return nil, fmt.Errorf("failed to create SSH session: %w", err)
	}

	stdout, err := session.StdoutPipe()
	if err != nil {
		_ = session.Close()
		_ = sshClient.Close()
		tunnel.close(ctx, f.apiClient)
		f.destroyHelperMachine(ctx, machine.ID)
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	f.logger.Debug("fly.cache.copyfromv.start", "volume", volumeName)

	if err := session.Start("tar cf - -C /volume ."); err != nil {
		_ = session.Close()
		_ = sshClient.Close()
		tunnel.close(ctx, f.apiClient)
		f.destroyHelperMachine(ctx, machine.ID)
		return nil, fmt.Errorf("failed to start tar: %w", err)
	}

	// Return a ReadCloser that cleans up all resources when closed
	return &cacheReader{
		ReadCloser: io.NopCloser(stdout),
		session:    session,
		sshClient:  sshClient,
		tunnel:     tunnel,
		machineID:  machine.ID,
		driver:     f,
	}, nil
}

// cacheReader wraps the SSH stdout stream and cleans up all resources on Close.
type cacheReader struct {
	io.ReadCloser
	session   *ssh.Session
	sshClient *ssh.Client
	tunnel    *flyTunnel
	machineID string
	driver    *Fly
}

func (r *cacheReader) Close() error {
	ctx := context.Background()

	// Wait for the tar command to finish
	_ = r.session.Wait()
	_ = r.session.Close()

	_ = r.sshClient.Close()
	r.tunnel.close(ctx, r.driver.apiClient)
	r.driver.destroyHelperMachine(ctx, r.machineID)

	r.driver.logger.Info("fly.cache.copyfromv.done")

	return nil
}

var _ cache.VolumeDataAccessor = (*Fly)(nil)

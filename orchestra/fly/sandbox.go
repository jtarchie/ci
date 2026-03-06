package fly

import (
	"context"
	"fmt"
	"io"
	"maps"
	"strings"
	"time"

	"github.com/jtarchie/ci/orchestra"
	fly "github.com/superfly/fly-go"
)

// FlySandbox keeps a Fly machine alive with "tail -f /dev/null" and dispatches
// exec calls via the Fly Machines exec API (POST /machines/{id}/exec).
type FlySandbox struct {
	machineID string
	driver    *Fly
}

var _ orchestra.Sandbox = (*FlySandbox)(nil)

// ID returns the Fly Machine ID.
func (s *FlySandbox) ID() string {
	return s.machineID
}

// Exec runs cmd inside the sandbox machine and writes its output to stdout/stderr.
// env and workDir apply only to this invocation.
func (s *FlySandbox) Exec(
	ctx context.Context,
	cmd []string,
	env map[string]string,
	workDir string,
	stdin io.Reader,
	stdout, stderr io.Writer,
) (orchestra.ContainerStatus, error) {
	// Build the command string, optionally wrapping with sh -c for env/workDir.
	execCmd := strings.Join(cmd, " ")

	if workDir != "" || len(env) > 0 {
		var parts []string

		for k, v := range env {
			parts = append(parts, fmt.Sprintf("export %s=%q", k, v))
		}

		if workDir != "" {
			parts = append(parts, "cd "+workDir)
		}

		parts = append(parts, "exec "+execCmd)
		execCmd = "/bin/sh -c '" + strings.Join(parts, " && ") + "'"
	}

	var stdinStr string
	if stdin != nil {
		b, err := io.ReadAll(stdin)
		if err != nil {
			return nil, fmt.Errorf("sandbox exec: failed to read stdin: %w", err)
		}

		stdinStr = string(b)
	}

	resp, err := s.driver.client.Exec(ctx, s.driver.appName, s.machineID, &fly.MachineExecRequest{
		Cmd:   execCmd,
		Stdin: stdinStr,
	})
	if err != nil {
		return nil, fmt.Errorf("sandbox exec: fly exec failed: %w", err)
	}

	if resp.StdOut != "" {
		_, _ = io.WriteString(stdout, resp.StdOut)
	}

	if resp.StdErr != "" {
		_, _ = io.WriteString(stderr, resp.StdErr)
	}

	return &containerStatus{
		done:     true,
		exitCode: int(resp.ExitCode),
	}, nil
}

// Cleanup stops and destroys the sandbox machine.
func (s *FlySandbox) Cleanup(ctx context.Context) error {
	machine, err := s.driver.client.Get(ctx, s.driver.appName, s.machineID)
	if err != nil {
		return nil // already gone
	}

	if machine.State == "started" || machine.State == "starting" {
		_ = s.driver.client.Kill(ctx, s.driver.appName, s.machineID)
		_ = s.driver.client.Wait(ctx, s.driver.appName, machine, "stopped", 30*time.Second)
	}

	return s.driver.client.Destroy(ctx, s.driver.appName, fly.RemoveMachineInput{
		ID:   s.machineID,
		Kill: true,
	}, "")
}

// StartSandbox implements orchestra.SandboxDriver.
// It launches a Fly Machine running "tail -f /dev/null" and returns a FlySandbox handle.
func (f *Fly) StartSandbox(ctx context.Context, task orchestra.Task) (orchestra.Sandbox, error) {
	logger := f.logger.With("taskID", task.ID)

	machineName := sanitizeAppName(fmt.Sprintf("%s-%s-sandbox", f.namespace, task.ID))

	env := make(map[string]string)
	maps.Copy(env, task.Env)

	var mounts []fly.MachineMount
	var mountDirs []string // subdirectory names to create under /workspace

	var sharedVolumeID string

	for _, taskMount := range task.Mounts {
		volume, err := f.CreateVolume(ctx, taskMount.Name, 1)
		if err != nil {
			return nil, fmt.Errorf("sandbox: failed to create volume: %w", err)
		}

		flyVolume, _ := volume.(*Volume)
		sharedVolumeID = flyVolume.id
		mountDirs = append(mountDirs, taskMount.Path)
	}

	if sharedVolumeID != "" {
		f.mu.Lock()
		oldMachineID, attached := f.volumeAttachments[sharedVolumeID]
		f.mu.Unlock()

		if attached {
			_ = f.client.Destroy(ctx, f.appName, fly.RemoveMachineInput{
				ID:   oldMachineID,
				Kill: true,
			}, "")
		}

		mounts = append(mounts, fly.MachineMount{
			Volume: sharedVolumeID,
			Path:   "/workspace",
		})
	}

	guest := &fly.MachineGuest{}

	if err := guest.SetSize(f.size); err != nil {
		guest.CPUKind = "shared"
		guest.CPUs = 1
		guest.MemoryMB = 256
	}

	if task.ContainerLimits.CPU > 0 {
		guest.CPUs = int(task.ContainerLimits.CPU)
	}

	if task.ContainerLimits.Memory > 0 {
		guest.MemoryMB = int(task.ContainerLimits.Memory / (1024 * 1024))
	}

	config := &fly.MachineConfig{
		Image: task.Image,
		Init: fly.MachineInit{
			Exec: []string{"tail", "-f", "/dev/null"},
		},
		Env:         env,
		Guest:       guest,
		AutoDestroy: false,
		Restart: &fly.MachineRestart{
			Policy: fly.MachineRestartPolicyNo,
		},
		Metadata: map[string]string{
			"orchestra.namespace": f.namespace,
		},
		Mounts: mounts,
	}

	machine, err := f.client.Launch(ctx, f.appName, fly.LaunchMachineInput{
		Name:   machineName,
		Config: config,
	})
	if err != nil {
		return nil, fmt.Errorf("sandbox: failed to launch machine: %w", err)
	}

	f.mu.Lock()
	f.machineIDs = append(f.machineIDs, machine.ID)
	f.mu.Unlock()

	// Wait for the machine to be in the started state.
	if err := f.client.Wait(ctx, f.appName, machine, "started", 2*time.Minute); err != nil {
		return nil, fmt.Errorf("sandbox: machine did not start: %w", err)
	}

	sandbox := &FlySandbox{
		machineID: machine.ID,
		driver:    f,
	}

	// Create mount subdirectories inside the shared workspace volume.
	if len(mountDirs) > 0 {
		var mkdirParts []string
		for _, dir := range mountDirs {
			mkdirParts = append(mkdirParts, "/workspace/"+dir)
		}

		_, err := f.client.Exec(ctx, f.appName, machine.ID, &fly.MachineExecRequest{
			Cmd: "mkdir -p " + strings.Join(mkdirParts, " "),
		})
		if err != nil {
			logger.Warn("sandbox.mkdir.error", "err", err)
		}
	}

	// Record volume attachment for future detach.
	if sharedVolumeID != "" {
		f.mu.Lock()
		f.volumeAttachments[sharedVolumeID] = machine.ID
		f.mu.Unlock()
	}

	logger.Debug("sandbox.started", "machineID", machine.ID)

	return sandbox, nil
}

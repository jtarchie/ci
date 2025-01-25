package orchestra

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/superfly/fly-go"
	"github.com/superfly/fly-go/flaps"
	"github.com/superfly/fly-go/tokens"
	"github.com/superfly/flyctl/logs"
	"go.uber.org/ratelimit"
)

type Fly struct {
	flyClient   *fly.Client
	flapsClient *flaps.Client
	namespace   string
}

var limiter = ratelimit.New(3)

func NewFly(namespace string) (Orchestrator, error) {
	accessToken, appName, err := getFlyDetails()
	if err != nil {
		return nil, fmt.Errorf("failed to get fly details: %w", err)
	}

	fly.SetBaseURL("https://api.fly.io")
	flyClient := fly.NewClient(
		accessToken,
		"flyctl",
		"2025.1.14-dev.1736892796",
		&logAdapter{slog.Default()},
	)

	flapsClient, err := flaps.NewWithOptions(context.Background(), flaps.NewClientOpts{
		AppName: appName,
		Tokens:  tokens.Parse(accessToken),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create flaps client: %w", err)
	}

	return &Fly{
		flapsClient: flapsClient,
		flyClient:   flyClient,
		namespace:   namespace,
	}, nil
}

type FlyContainer struct {
	flapsClient *flaps.Client
	flyClient   *fly.Client
	id          string
	instanceID  string
	task        Task
}

func (f *Fly) Name() string {
	return "fly"
}

func (f *Fly) RunContainer(ctx context.Context, task Task) (Container, error) {
	containerName := fmt.Sprintf("%s-%s", f.namespace, task.ID)

	_ = limiter.Take()

	machines, err := f.flapsClient.ListActive(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list active machines: %w", err)
	}

	for _, machine := range machines {
		if machine.Name == containerName {
			return &FlyContainer{
				flapsClient: f.flapsClient,
				flyClient:   f.flyClient,
				id:          machine.ID,
				instanceID:  machine.InstanceID,
				task:        task,
			}, nil
		}
	}

	_ = limiter.Take()

	response, err := f.flapsClient.Launch(ctx, fly.LaunchMachineInput{
		Name: containerName,
		Config: &fly.MachineConfig{
			// AutoDestroy: true,
			Image: task.Image,
			Processes: []fly.MachineProcess{
				{
					CmdOverride: task.Command,
				},
			},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to launch container: %w", err)
	}
	return &FlyContainer{
		flapsClient: f.flapsClient,
		flyClient:   f.flyClient,
		id:          response.ID,
		instanceID:  response.InstanceID,
		task:        task,
	}, nil
}

// Cleanup implements Container.
func (f *FlyContainer) Cleanup(ctx context.Context) error {
	_ = limiter.Take()

	err := f.flapsClient.Destroy(ctx, fly.RemoveMachineInput{
		ID:   f.id,
		Kill: true,
	}, "")
	if err != nil {
		return fmt.Errorf("failed to destroy machine: %w", err)
	}

	return nil
}

// Logs implements Container.
func (f *FlyContainer) Logs(ctx context.Context, stdout io.Writer, stderr io.Writer) error {
	_, appName, err := getFlyDetails()
	if err != nil {
		return fmt.Errorf("failed to get fly details: %w", err)
	}

	out := make(chan logs.LogEntry)
	var outErr error

	go func() {
		defer close(out)

		_ = limiter.Take()

		outErr = logs.Poll(ctx, out, f.flyClient, &logs.LogOptions{
			AppName: appName,
			VMID:    f.id,
			NoTail:  true,
		})
	}()

	for entry := range out {
		_, _ = io.WriteString(stdout, entry.Message)
	}

	if outErr != nil {
		return fmt.Errorf("failed to poll logs: %w", outErr)
	}

	return nil
}

// Status implements Container.
func (f *FlyContainer) Status(ctx context.Context) (ContainerStatus, error) {
	_ = limiter.Take()

	machine, err := f.flapsClient.Get(ctx, f.id)
	if err != nil {
		return nil, fmt.Errorf("failed to get machine: %w", err)
	}

	return &FlyContainerStatus{
		machine: machine,
	}, nil
}

type FlyContainerStatus struct {
	machine *fly.Machine
}

// ExitCode implements ContainerStatus.
func (f *FlyContainerStatus) ExitCode() int {
	for _, event := range f.machine.Events {
		if event.Request == nil {
			continue
		}

		if status, err := event.Request.GetExitCode(); err == nil {
			return status
		}
	}

	return -1
}

// IsDone implements ContainerStatus.
func (f *FlyContainerStatus) IsDone() bool {
	for _, event := range f.machine.Events {
		if event.Request == nil {
			continue
		}

		if _, err := event.Request.GetExitCode(); err == nil {
			return true
		}
	}

	return false
}

// logAdapter adapts slog.Logger to the Logger interface.
type logAdapter struct {
	logger *slog.Logger
}

// NewSlogAdapter creates a new instance of SlogAdapter.
func NewSlogAdapter(logger *slog.Logger) *logAdapter {
	return &logAdapter{logger: logger}
}

// Debug logs a message at Debug level.
func (a *logAdapter) Debug(v ...interface{}) {
	a.logger.Debug(fmt.Sprint(v...))
}

// Debugf logs a formatted message at Debug level.
func (a *logAdapter) Debugf(format string, v ...interface{}) {
	a.logger.Debug(fmt.Sprintf(format, v...))
}

func getFlyDetails() (string, string, error) {
	accessToken := os.Getenv("FLY_ACCESS_TOKEN")
	if accessToken == "" {
		return "", "", fmt.Errorf("FLY_ACCESS_TOKEN must be set")
	}

	appName := os.Getenv("FLY_APP_NAME")
	if appName == "" {
		return "", "", fmt.Errorf("FLY_APP_NAME must be set")
	}

	return accessToken, appName, nil
}

func init() {
	Add("fly", NewFly)
}

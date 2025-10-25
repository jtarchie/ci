package orchestra

import "io"

type Mount struct {
	Name string
	Path string
}

type Mounts []Mount

type ContainerLimits struct {
	// CPU shares (0 means unlimited)
	CPU int64
	// Memory in bytes (0 means unlimited)
	Memory int64
}

// across all drivers.
type Task struct {
	Command         []string
	ContainerLimits ContainerLimits
	Env             map[string]string
	ID              string
	Image           string
	Mounts          Mounts
	Privileged      bool
	Stdin           io.Reader
	User            string
}

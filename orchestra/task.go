package orchestra

import "io"

type Mount struct {
	Name string
	Path string
}

type Mounts []Mount

type Task struct {
	Command []string
	Env     map[string]string
	ID      string
	Image   string
	Mounts  Mounts
	Stdin   io.Reader
}

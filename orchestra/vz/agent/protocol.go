package agent

// VsockPort is the well-known vsock port the agent listens on inside the guest.
const VsockPort = 1024

// Request represents a JSON-encoded request from host to guest agent.
type Request struct {
	Type      string   `json:"type"`
	Path      string   `json:"path,omitempty"`
	Args      []string `json:"args,omitempty"`
	Env       []string `json:"env,omitempty"`
	StdinData string   `json:"stdin_data,omitempty"`
	PID       int      `json:"pid,omitempty"`
}

// Response represents a JSON-encoded response from guest agent to host.
type Response struct {
	OK       bool   `json:"ok"`
	Error    string `json:"error,omitempty"`
	PID      int    `json:"pid,omitempty"`
	Exited   bool   `json:"exited,omitempty"`
	ExitCode int    `json:"exit_code,omitempty"`
	Stdout   string `json:"stdout,omitempty"`
	Stderr   string `json:"stderr,omitempty"`
}

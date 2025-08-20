package tools

// ToolSpec defines how to execute a tool process.
// Only fields required by the runner and tests are included here.
// Additional fields may be added in future as needed by the tool ecosystem.
type ToolSpec struct {
	Name           string   // human-readable tool name for audit
	Command        []string // argv vector: program and its arguments
	TimeoutSec     int      // per-invocation timeout in seconds; 0 uses default
	EnvPassthrough []string // environment variable keys to pass through to the child
}

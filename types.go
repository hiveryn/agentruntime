package agentruntime

import "time"

type AgentKind string

const (
	AgentClaude   AgentKind = "claude"
	AgentCodex    AgentKind = "codex"
	AgentOpenCode AgentKind = "opencode"
)

type Status string

const (
	StatusStarting      Status = "starting"
	StatusWorking       Status = "working"
	StatusIdle          Status = "idle"
	StatusAwaitingInput Status = "awaiting_input"
	StatusError         Status = "error"
	StatusEnded         Status = "ended"
)

type NativeSessionRole string

const (
	NativeSessionRoleUnknown    NativeSessionRole = ""
	NativeSessionRolePrimary    NativeSessionRole = "primary"
	NativeSessionRoleSubsession NativeSessionRole = "subsession"
)

type StartRequest struct {
	ID           string
	Agent        AgentKind
	Command      string
	Args         []string
	Env          map[string]string
	Workdir      string
	Prompt       string
	Instructions string
	MCPServers   []MCPServerConfig
}

type Event struct {
	ID                string
	NativeID          string
	PrimaryNativeID   string
	NativeSessionRole NativeSessionRole
	Agent             AgentKind
	Status            Status
	Tool              string
	Message           string
	At                time.Time
	Metadata          map[string]string
	Raw               map[string]any
}

type LaunchSpec struct {
	Command      string
	Args         []string
	Env          map[string]string
	Workdir      string
	CleanupPaths []string
}

type MCPServerConfig struct {
	Name              string
	Command           string
	Args              []string
	CWD               string
	Env               map[string]string
	URL               string
	BearerTokenEnvVar string
}

type HookCommand struct {
	Command       string
	Endpoint      string
	Timeout       time.Duration
	StatusMessage string
}

type SetupRequest struct {
	Marker     string
	ConfigRoot string
	Hook       HookCommand
}

type SetupResult struct {
	Changed bool
	Paths   []string
}

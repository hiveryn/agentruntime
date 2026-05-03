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
	ID                  string
	Agent               AgentKind
	Command             string
	Args                []string
	Env                 map[string]string
	Workdir             string
	Prompt              string
	Instructions        string
	MCPServers          []MCPServerConfig
	OpenCodeProfile     string                      // OpenCode --agent profile name; leave empty for OpenCode default
	OpenCodeAgentConfig map[string]OpenCodeAgentConfig // OpenCode agent profile definitions merged into OPENCODE_CONFIG_CONTENT
	Resume              bool                        // Resume an existing session instead of starting a new one
	ResumeID            string                      // Native session ID to resume; if empty, resumes the most recent session
}

// OpenCodeAgentConfig defines an OpenCode agent profile entry for the config agent section.
// Prompt here is the agent definition prompt, independent of StartRequest.Prompt (kickoff) and StartRequest.Instructions (additive).
type OpenCodeAgentConfig struct {
	Description string            `json:"description"`
	Mode        string            `json:"mode"`
	Prompt      string            `json:"prompt,omitempty"`
	Permission  map[string]string `json:"permission"`
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

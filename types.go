package agentruntime

import "time"

type AgentKind string

const (
	AgentClaude   AgentKind = "claude"
	AgentCodex    AgentKind = "codex"
	AgentOpenCode AgentKind = "opencode"
)

// Mode is the execution posture of a session.
type Mode string

const (
	ModeBuild Mode = "build" // Full execution (default). Empty Mode is treated as build.
	ModePlan  Mode = "plan"  // Read-only planning/analysis.
)

// RunMode is the interactivity posture of a session.
type RunMode string

const (
	RunInteractive RunMode = ""         // default: launch an interactive TUI session
	RunHeadless    RunMode = "headless" // non-interactive: run to completion, print to stdout, exit
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
	ID      string
	Agent   AgentKind
	Command string
	Args    []string
	Env     map[string]string
	Workdir string
	Prompt  string
	Model   string  // Agent model selector, translated to the agent-specific flag (e.g. --model). Agent-specific value form (claude/codex: bare id; opencode: provider/model).
	Yolo    bool    // Full autonomy: skip approvals/permission prompts. Translated to the agent-specific flag (claude --dangerously-skip-permissions, codex --dangerously-bypass-approvals-and-sandbox, opencode permission:allow).
	Mode    Mode    // Execution mode (build or plan). Empty defaults to build. Plan is unsupported by codex.
	RunMode RunMode // Interactivity posture. Empty defaults to interactive. Headless runs the agent
	// non-interactively to completion and exits (claude --print, codex exec, opencode run); the consumer
	// captures stdout and the exit code rather than a long-lived session.
	Instructions        string
	MCPServers          []MCPServerConfig
	OpenCodeAgentConfig map[string]OpenCodeAgentConfig // OpenCode agent profile definitions merged into OPENCODE_CONFIG_CONTENT
	Resume              bool                           // Resume an existing session instead of starting a new one
	ResumeID            string                         // Native session ID to resume; if empty, resumes the most recent session
}

// OpenCodeAgentConfig defines an OpenCode agent profile entry for the config agent section.
// Prompt here is the agent definition prompt, independent of StartRequest.Prompt (kickoff) and StartRequest.Instructions (additive).
type OpenCodeAgentConfig struct {
	Description string            `json:"description"`
	Mode        string            `json:"mode"`
	Prompt      string            `json:"prompt,omitempty"`
	Permission  map[string]string `json:"permission,omitempty"`
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
	// NativeSessionID is the agent's own session identifier when it is known
	// before launch. Claude mints its --session-id UUID in PrepareLaunch, so it
	// is populated here. Codex/OpenCode mint ids at runtime, so this is empty
	// pre-launch for them (a known, expected asymmetry).
	NativeSessionID string
}

// Usage is provider-normalized token accounting for one session. Dollar cost is
// out of scope: agentruntime returns tokens and the resolved model id; the
// consumer multiplies by its own price table.
type Usage struct {
	InputTokens      int    // uncached prompt tokens
	OutputTokens     int    // generated tokens
	CacheWriteTokens int    // claude: cache_creation_input_tokens
	CacheReadTokens  int    // claude: cache_read_input_tokens
	Model            string // resolved model id (last non-synthetic message)
	Messages         int    // assistant turns counted
}

// LocateRequest carries the inputs needed to resolve a session's transcript
// path. NativeSessionID is the agent's session id; ConfigRoot is the adapter's
// resolved config directory (see Adapter.ConfigRoot); Workdir is the session's
// working directory (used by codex/opencode discovery, unused for claude).
type LocateRequest struct {
	NativeSessionID string
	ConfigRoot      string
	Workdir         string
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

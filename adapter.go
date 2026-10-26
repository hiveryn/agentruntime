package agentruntime

import "context"

type Adapter interface {
	Agent() AgentKind
	// ConfigRoot resolves the agent's config directory from the variant
	// environment, returning "" when the agent uses its default location.
	// Each adapter owns the env-var name it reads (e.g. CLAUDE_CONFIG_DIR,
	// CODEX_HOME).
	ConfigRoot(env map[string]string) string
	PrepareLaunch(context.Context, StartRequest) (LaunchSpec, error)
	EnsureSetup(context.Context, SetupRequest) (SetupResult, error)
	RemoveSetup(context.Context, SetupRequest) (SetupResult, error)
	NormalizeEvent(context.Context, []byte) (*Event, error)
	// LocateTranscript resolves the absolute transcript path for a session. It
	// is the primary path-discovery mechanism for codex/opencode; for claude it
	// returns the path constructed from ConfigRoot and the native session id.
	LocateTranscript(context.Context, LocateRequest) (string, error)
	// ParseUsage reads a native session transcript and returns normalized usage.
	ParseUsage(ctx context.Context, transcriptPath string) (Usage, error)
}

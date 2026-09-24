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

// AttentionDetector is implemented by adapters that can tell, from explicit
// provider evidence, that an interactive agent is blocked on the user. Hooks
// report the prompts they cover as StatusAwaitingInput; InspectScreen covers
// prompts the agent shows before any hook fires, or without one, by
// recognizing them on the agent's rendered terminal screen. It never infers a
// wait from silence, idleness or a missing hook.
type AttentionDetector interface {
	// InspectScreen returns the prompt the agent is currently showing on its
	// rendered terminal screen (one string per row, top to bottom), or nil
	// when it recognizes none. A recognized prompt is structural — the
	// provider's own dialog or banner in its live position — so text merely
	// quoted in the conversation or left in scrollback is not reported.
	InspectScreen(lines []string) *Attention
	// AttentionCoverage states which waits the adapter can detect (by hook and
	// on screen) and which it cannot, for callers to show next to a result
	// that detected nothing.
	AttentionCoverage() string
}

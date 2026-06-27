package codex

import (
	"context"

	"github.com/hiveryn/agentruntime"
)

type Options struct{}

type Adapter struct {
	options Options
}

func New(options Options) *Adapter {
	return &Adapter{options: options}
}

func DefaultOptions() Options {
	return Options{}
}

func (a *Adapter) Agent() agentruntime.AgentKind {
	return agentruntime.AgentCodex
}

// ConfigRoot returns the variant's CODEX_HOME, which Codex treats as the
// .codex-equivalent directory.
func (a *Adapter) ConfigRoot(env map[string]string) string {
	return env["CODEX_HOME"]
}

// LocateTranscript is not yet implemented for codex (see its own ticket).
func (a *Adapter) LocateTranscript(context.Context, agentruntime.LocateRequest) (string, error) {
	return "", agentruntime.ErrUsageNotImplemented
}

// ParseUsage is not yet implemented for codex (see its own ticket).
func (a *Adapter) ParseUsage(context.Context, string) (agentruntime.Usage, error) {
	return agentruntime.Usage{}, agentruntime.ErrUsageNotImplemented
}

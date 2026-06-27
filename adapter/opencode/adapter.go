package opencode

import (
	"github.com/hiveryn/agentruntime"
)

// Options configures the OpenCode adapter.
type Options struct{}

// DefaultOptions returns the default OpenCode adapter options.
func DefaultOptions() Options { return Options{} }

// Adapter implements agentruntime.Adapter for OpenCode.
type Adapter struct {
	options Options
}

// New creates a new OpenCode adapter with the given options.
func New(opts Options) *Adapter { return &Adapter{options: opts} }

// Agent returns the agent kind this adapter handles.
func (a *Adapter) Agent() agentruntime.AgentKind { return agentruntime.AgentOpenCode }

// ConfigRoot returns "" — opencode resolves its plugin directory from
// XDG_CONFIG_HOME / the home directory rather than a variant env var. Usage
// accounting (LocateTranscript/ParseUsage, in usage.go) resolves opencode's XDG
// data dir separately, since the database lives there, not under the config root.
func (a *Adapter) ConfigRoot(map[string]string) string { return "" }

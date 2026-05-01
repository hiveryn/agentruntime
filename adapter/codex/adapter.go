package codex

import "github.com/hiveryn/agentruntime"

type Options struct {
	NoAltScreen      bool
	EnableHooks      bool
	Sandbox          string
	ApprovalPolicy   string
	SkipGitRepoCheck bool
	Model            string
	Profile          string
}

type Adapter struct {
	options Options
}

func New(options Options) *Adapter {
	return &Adapter{options: options}
}

func DefaultOptions() Options {
	return Options{
		NoAltScreen:    true,
		EnableHooks:    true,
		Sandbox:        "read-only",
		ApprovalPolicy: "never",
	}
}

func ApplyMCPApprovalDefaults(server agentruntime.MCPServerConfig) agentruntime.MCPServerConfig {
	if server.DefaultToolsApprovalMode == "" {
		server.DefaultToolsApprovalMode = "approve"
	}
	if server.ApprovalMode == "" {
		server.ApprovalMode = "approve"
	}
	return server
}

func (a *Adapter) Agent() agentruntime.AgentKind {
	return agentruntime.AgentCodex
}

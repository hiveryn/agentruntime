package codex

import "github.com/hiveryn/agentruntime"

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

package opencode

import "github.com/hiveryn/agentruntime"

// InspectScreen recognizes nothing: OpenCode shows no folder-trust dialog, and
// its permission and question prompts are reported by plugin events.
func (a *Adapter) InspectScreen([]string) *agentruntime.Attention { return nil }

// AttentionCoverage states what OpenCode waits are detected.
func (a *Adapter) AttentionCoverage() string {
	return "OpenCode: permission and question prompts are detected from its plugin events. Nothing is recognized on the terminal screen, so dialogs such as its update prompt are not detected."
}

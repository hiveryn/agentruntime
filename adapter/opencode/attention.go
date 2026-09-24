package opencode

import "github.com/hiveryn/agentruntime"

// InspectScreen recognizes nothing: OpenCode shows no folder-trust dialog, its
// startup update modal does not block the agent, and its permission and
// question prompts are reported by plugin events. A session resumed after an
// interrupted turn shows no interruption notice to recognize.
func (a *Adapter) InspectScreen([]string) *agentruntime.Attention { return nil }

// AttentionCoverage states what OpenCode waits are detected.
func (a *Adapter) AttentionCoverage() string {
	return "OpenCode: permission and question prompts are detected from its plugin events. Its startup update prompt does not block the agent and is not reported. Nothing is recognized on the terminal screen: a resumed session waiting after an interrupted turn shows no interruption notice and is not detected, nor are other dialogs."
}

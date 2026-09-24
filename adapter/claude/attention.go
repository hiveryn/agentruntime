package claude

import (
	"github.com/hiveryn/agentruntime"
	"github.com/hiveryn/agentruntime/internal/screen"
)

// Attention reasons Claude Code reports on screen. Both dialogs block startup
// before SessionStart, so no hook reports them.
const (
	AttentionFolderTrust       = "folder_trust"
	AttentionBypassPermissions = "bypass_permissions_warning"
)

const dialogFooter = "Enter to confirm · Esc to cancel"

// InspectScreen recognizes Claude Code's startup folder-trust dialog and its
// Bypass Permissions warning. Permission and elicitation prompts are reported
// by hooks instead.
func (a *Adapter) InspectScreen(lines []string) *agentruntime.Attention {
	rows := screen.Rows(lines)
	switch {
	case screen.Dialog(rows, "Accessing workspace:", dialogFooter, "Yes, I trust this folder"):
		return &agentruntime.Attention{Reason: AttentionFolderTrust, Message: "Claude Code is asking whether to trust this folder before it starts."}
	case screen.Dialog(rows, "WARNING: Claude Code running in Bypass Permissions mode", dialogFooter, "Yes, I accept"):
		return &agentruntime.Attention{Reason: AttentionBypassPermissions, Message: "Claude Code is asking to accept Bypass Permissions mode before it starts."}
	}
	return nil
}

// AttentionCoverage states what Claude Code waits are detected.
func (a *Adapter) AttentionCoverage() string {
	return "Claude Code: permission and elicitation prompts are detected from its hooks; the folder-trust and Bypass Permissions startup dialogs are recognized on the terminal screen. A resumed conversation waiting at its prompt, and other dialogs, are not detected."
}

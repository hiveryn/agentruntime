package claude

import (
	"regexp"
	"strings"

	"github.com/hiveryn/agentruntime"
	"github.com/hiveryn/agentruntime/internal/screen"
)

// Attention reasons Claude Code reports on screen. The startup dialogs block
// before SessionStart, and a resumed conversation whose last tool call was cut
// off fires only SessionStart, so no hook reports any of them.
const (
	AttentionFolderTrust             = "folder_trust"
	AttentionBypassPermissions       = "bypass_permissions_warning"
	AttentionConversationInterrupted = "conversation_interrupted"
)

const (
	dialogFooter      = "Enter to confirm · Esc to cancel"
	interruptedNotice = "⎿ Interrupted · What should Claude do instead?"
)

// InspectScreen recognizes Claude Code's startup folder-trust dialog, its
// Bypass Permissions warning, and a conversation left waiting after an
// interrupted tool call. Permission and elicitation prompts are reported by
// hooks instead.
func (a *Adapter) InspectScreen(lines []string) *agentruntime.Attention {
	rows := screen.Rows(lines)
	switch {
	case screen.Dialog(rows, "Accessing workspace:", dialogFooter, "Yes, I trust this folder"):
		return &agentruntime.Attention{Reason: AttentionFolderTrust, Message: "Claude Code is asking whether to trust this folder before it starts."}
	case screen.Dialog(rows, "WARNING: Claude Code running in Bypass Permissions mode", dialogFooter, "Yes, I accept"):
		return &agentruntime.Attention{Reason: AttentionBypassPermissions, Message: "Claude Code is asking to accept Bypass Permissions mode before it starts."}
	case interruptedAwaitingMessage(rows):
		return &agentruntime.Attention{Reason: AttentionConversationInterrupted, Message: "Claude Code's last turn was interrupted and it is waiting for a message before it continues."}
	}
	return nil
}

// effortHint is the composer's transient "● high · /effort" hint, shown
// right-aligned just above the composer for a few seconds after startup.
var effortHint = regexp.MustCompile(`^(● )?\w+ · /effort$`)

// interruptedAwaitingMessage reports whether Claude's interrupted notice is
// the last history row, directly above the composer's top rule (the rule row
// just above the last row starting with "❯"). Once the user sends a message,
// their prompt and Claude's reply sit between the two, so a notice left in
// history no longer matches. Text still being typed into the composer does
// not count as sent.
func interruptedAwaitingMessage(rows []string) bool {
	rule := -1
	for i := len(rows) - 1; i > 0; i-- {
		if strings.HasPrefix(rows[i], "❯") && isRule(rows[i-1]) {
			rule = i - 1
			break
		}
	}
	i := rule - 1
	for i >= 0 && (rows[i] == "" || effortHint.MatchString(rows[i])) {
		i--
	}
	return i >= 0 && rows[i] == interruptedNotice
}

func isRule(row string) bool {
	return row != "" && strings.Trim(row, "─") == ""
}

// AttentionCoverage states what Claude Code waits are detected.
func (a *Adapter) AttentionCoverage() string {
	return "Claude Code: permission and elicitation prompts are detected from its hooks; the folder-trust and Bypass Permissions startup dialogs, and a resumed conversation waiting after an interrupted tool call, are recognized on the terminal screen. A resumed conversation whose interrupted reply was dropped, or whose background command was stopped, shows no interruption notice and is not detected; nor are other dialogs."
}

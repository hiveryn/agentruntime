package codex

import (
	"strings"

	"github.com/hiveryn/agentruntime"
	"github.com/hiveryn/agentruntime/internal/screen"
)

// Attention reasons Codex reports on screen. Neither prompt fires a hook:
// folder trust blocks startup before SessionStart, and a resumed conversation
// whose last turn was cut off fires nothing until the user sends a message.
// (Codex's hook-review dialog never shows: launches bypass hook trust.)
const (
	AttentionFolderTrust             = "folder_trust"
	AttentionConversationInterrupted = "conversation_interrupted"
)

const interruptedBanner = "■ Conversation interrupted"

// InspectScreen recognizes Codex's startup folder-trust dialog and a conversation
// left waiting after an interrupted turn. Approval prompts are reported by the
// PermissionRequest hook instead.
func (a *Adapter) InspectScreen(lines []string) *agentruntime.Attention {
	rows := screen.Rows(lines)
	switch {
	case screen.Dialog(rows, "Folder access", "enter continue · esc quit", "Trust and continue"):
		return &agentruntime.Attention{Reason: AttentionFolderTrust, Message: "Codex is asking whether to trust this folder before it starts."}
	case interruptedAwaitingMessage(rows):
		return &agentruntime.Attention{Reason: AttentionConversationInterrupted, Message: "Codex's last turn was interrupted and it is waiting for a message before it continues."}
	}
	return nil
}

// interruptedAwaitingMessage reports whether the interrupted banner is the
// last history cell, directly above the composer (the last row starting with
// "›"). Once the user sends a message, their message cell and Codex's working
// status sit between the two, so a banner left in history no longer matches.
// Text still being typed into the composer does not count as sent.
func interruptedAwaitingMessage(rows []string) bool {
	composer := -1
	for i := len(rows) - 1; i >= 0; i-- {
		if strings.HasPrefix(rows[i], "›") {
			composer = i
			break
		}
	}
	if composer < 0 {
		return false
	}
	i := composer - 1
	for i >= 0 && rows[i] == "" {
		i--
	}
	// Walk up the last history cell: its wrapped continuation rows carry no
	// cell marker; the first marked row is where the cell starts.
	for i >= 0 && rows[i] != "" && !cellStart(rows[i]) {
		i--
	}
	return i >= 0 && strings.HasPrefix(rows[i], interruptedBanner)
}

func cellStart(row string) bool {
	return strings.HasPrefix(row, "■") || strings.HasPrefix(row, "•") || strings.HasPrefix(row, "›") || strings.HasPrefix(row, "└")
}

// AttentionCoverage states what Codex waits are detected.
func (a *Adapter) AttentionCoverage() string {
	return "Codex: approval prompts are detected from its PermissionRequest hook; the folder-trust startup dialog and a resumed conversation waiting after an interrupted turn are recognized on the terminal screen. Other prompts and dialogs are not detected."
}

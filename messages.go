package agentruntime

import (
	"context"
	"time"
)

// Message delivery: handing a plain-text message into an existing agent
// session through the provider's own native route, never by typing into its
// terminal. The caller owns durability, routing, retry and what the messages
// mean; adapters own the provider mechanics. Evidence and the behaviour matrix
// behind this contract are in research/2026-10-20-message-delivery/.
//
// Delivery is at-least-once from the caller's side. The caller keeps a stable
// MessageID per message, checks the provider's own record (ConfirmMessage)
// before any redelivery and redelivers under the same identity, which makes
// delivery effectively once outside the documented provider crash windows. No
// adapter promises exactly-once, and none promises that a message appearing
// in the conversation was acted upon.

// DefaultMessageHookTimeout bounds each Claude asyncRewake waiter. The waiter
// is the only way to wake an idle Claude session, and once it expires nothing
// re-arms the session until its next Stop or SessionStart, so it must cover
// the longest idle period the caller expects.
const DefaultMessageHookTimeout = 24 * time.Hour

// MessageDelivery asks the adapter to add the per-process launch plumbing the
// caller needs to deliver messages into the session later. It is applied on
// new and resumed launches alike; nil leaves the launch as before.
//
// claude: per-launch --settings adding SessionStart and Stop asyncRewake
// command hooks whose waiter long-polls $AGENTRUNTIME_HOOK_ENDPOINT/claude/wait
// (see ingest.Mailbox) and wakes the model with the message on exit code 2.
// opencode: --port ServerPort and OPENCODE_SERVER_PASSWORD=ServerPassword, so
// the caller can reach the TUI's embedded session server; both are required.
// codex: nothing at launch — its durable thread queue is reachable from
// CODEX_HOME.
type MessageDelivery struct {
	// HookWaitTimeout is the Claude waiter hook's timeout; zero means
	// DefaultMessageHookTimeout.
	HookWaitTimeout time.Duration
	// ServerPort is the loopback port OpenCode's session server listens on.
	ServerPort int
	// ServerPassword protects that server (basic auth, user "opencode").
	ServerPassword string
}

// MessageDeliveryMode says who moves the message into the provider.
type MessageDeliveryMode string

const (
	// MessageDeliveryPull: the provider arms a waiter in the caller's
	// ingest.Mailbox and the caller hands the message to it (Claude).
	MessageDeliveryPull MessageDeliveryMode = "pull"
	// MessageDeliveryPush: the caller pushes through DeliverMessage (Codex,
	// OpenCode).
	MessageDeliveryPush MessageDeliveryMode = "push"
)

// MessageCapability describes how an adapter delivers messages and what the
// caller must do around it.
type MessageCapability struct {
	// Mechanism names the provider route, for diagnostics.
	Mechanism string
	Mode      MessageDeliveryMode
	// GateOnIdle: the caller must hand over or push only while the session is
	// idle. Otherwise the message joins the running turn and the model tends
	// to answer it in place of the task's own final reply. A provider that
	// holds queued messages until its turn ends (Codex) does not need gating,
	// except after an interrupted turn, where the queue waits for the user's
	// next completed turn.
	GateOnIdle bool
	// Coverage states what the route and its receipt check do and do not cover,
	// for callers to show next to a stuck or lost message.
	Coverage string
}

// DeliverRequest pushes one message into a running session.
type DeliverRequest struct {
	// Env is the launch environment of the session (its CODEX_HOME,
	// CLAUDE_CONFIG_DIR, ...). Command is the provider executable, empty for
	// the default.
	Env     map[string]string
	Command string
	// NativeSessionID is the provider's own session (thread) id.
	NativeSessionID string
	// ServerPort and ServerPassword are the MessageDelivery launch values
	// (OpenCode).
	ServerPort     int
	ServerPassword string
	// MessageID is the caller's stable identity of the message. It is carried
	// into the provider (Codex clientUserMessageId) or embedded in the text,
	// so ConfirmMessage can find it.
	MessageID string
	// ProviderRef is the provider-side identity minted by an earlier attempt
	// of this same message to this same session, or empty on the first
	// attempt (OpenCode message id).
	ProviderRef string
	Text        string
}

// DeliverOutcome is what a push achieved.
type DeliverOutcome string

const (
	// DeliverAccepted: the provider accepted the message for its queue or
	// run loop. Acceptance is not processing — confirm later.
	DeliverAccepted DeliverOutcome = "accepted"
	// DeliverAlreadyPresent: the provider already held or recorded this
	// message, so nothing was sent again.
	DeliverAlreadyPresent DeliverOutcome = "already_present"
)

type DeliverResult struct {
	Outcome DeliverOutcome
	// ProviderRef is the provider-side identity to keep for retries and
	// ConfirmMessage.
	ProviderRef string
}

// ConfirmRequest asks whether a message reached the conversation.
// TranscriptHandle comes from LocateTranscript for the target session; Env,
// Command and NativeSessionID let Codex also inspect its queue.
type ConfirmRequest struct {
	TranscriptHandle string
	Env              map[string]string
	Command          string
	NativeSessionID  string
	MessageID        string
	ProviderRef      string
}

// MessageReceipt is the provider's own record of a message. Appeared means the
// message is in the conversation record; Acknowledged means the agent produced
// a reply after it. Neither means the agent did what the message asked. Queued
// means the provider still holds it undelivered (Codex queue).
//
// RecordedThrough is the time of the newest entry in the record that was
// inspected (zero when the record carries no times, or is empty). A receipt
// that did not appear is authoritative only when the record reaches past the
// hand-off: a record ending before it is stale evidence — an old segment, an
// unflushed file, the wrong file — and says nothing about the message.
type MessageReceipt struct {
	Appeared        bool
	Acknowledged    bool
	Queued          bool
	RecordedThrough time.Time
}

// MessageDeliverer is implemented by adapters that can deliver messages into
// their sessions. Pull-mode adapters return an error from DeliverMessage: the
// caller hands the message to the armed waiter instead.
type MessageDeliverer interface {
	MessageCapability() MessageCapability
	DeliverMessage(context.Context, DeliverRequest) (DeliverResult, error)
	ConfirmMessage(context.Context, ConfirmRequest) (MessageReceipt, error)
}

// MessageHookTimeout resolves the Claude waiter timeout of a request.
func (d *MessageDelivery) MessageHookTimeout() time.Duration {
	if d == nil || d.HookWaitTimeout <= 0 {
		return DefaultMessageHookTimeout
	}
	return d.HookWaitTimeout
}

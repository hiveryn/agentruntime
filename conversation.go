package agentruntime

import (
	"context"
	"io"
	"time"
)

// ConversationAdapter prepares a single-submitter structured transport. The
// caller owns the process, pipes, cancellation, durable queue and receipts.
type ConversationAdapter interface {
	PrepareConversation(context.Context, StartRequest) (ConversationSpec, error)
}

type ConversationSpec struct {
	Launch  LaunchSpec
	Connect func(context.Context, io.Reader, io.Writer) (Conversation, error)
}

// Turn blocks until native completion or transport failure. A transport error
// is ambiguous, never permission to replay. Observe callbacks must be persisted
// before returning; a persistence error stops consuming the turn.
type Conversation interface {
	NativeID() string
	Turn(context.Context, ConversationInput, func(ConversationEvent) error) error
}

type ConversationInput struct{ ID, Text, Origin string }
type ConversationEvent struct {
	ID     string
	TurnID string
	Role   string
	Text   string
	Tool   string
	Status string
	At     time.Time
	// Complete is explicit native turn completion, independent of tool errors.
	Complete bool
	// Acceptance is set on the turn's first event that establishes the
	// provider accepted the input, naming that evidence (for example
	// "claude_stream_output", "codex_turn_start", "opencode_prompt_async").
	// Acceptance is not completion; a turn that fails before any such event
	// leaves acceptance unknown.
	Acceptance string
}

// ConversationReconciler reads provider-owned records after the previous owner
// is gone. Only explicit native terminal evidence may settle an ambiguous turn;
// missing records and incomplete turns never authorize resubmission.
type ConversationReconciler interface {
	Reconcile(context.Context, ConversationInput, string) ([]ConversationEvent, error)
}

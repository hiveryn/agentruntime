package opencode

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/hiveryn/agentruntime"
)

var droppedByDesign = map[string]struct{}{
	"session.updated":               {},
	"session.deleted":               {},
	"session.diff":                  {},
	"session.compacted":             {},
	"message.updated":               {},
	"message.removed":               {},
	"message.part.updated":          {},
	"message.part.removed":          {},
	"message.part.delta":            {},
	"permission.replied":            {},
	"file.edited":                   {},
	"todo.updated":                  {},
	"command.executed":              {},
	"installation.update-available": {},
	"server.instance.disposed":      {},
}

type envelope struct {
	HookEventName        string         `json:"hook_event_name"`
	SessionID            string         `json:"session_id"`
	ParentSessionID      string         `json:"parent_session_id"`
	SessionCorrelationID string         `json:"agentruntime_session_id"`
	Payload              map[string]any `json:"payload"`
}

func (a *Adapter) NormalizeEvent(_ context.Context, data []byte) (*agentruntime.Event, error) {
	var env envelope
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, fmt.Errorf("opencode: decode envelope: %w", err)
	}

	name := env.HookEventName
	if name == "" {
		return nil, fmt.Errorf("opencode: missing hook_event_name")
	}

	if _, drop := droppedByDesign[name]; drop {
		return nil, nil
	}

	status, ok := opencodeStatus(name, env.Payload)
	if !ok {
		return nil, nil
	}

	nativeID := env.SessionID
	var primaryNativeID string
	role := agentruntime.NativeSessionRoleUnknown

	// Only session.created carries parent information. For all other events
	// leave PrimaryNativeID and Role empty so the ingest receiver can classify
	// them correctly using its stored (agent, callerID) → primaryNativeID map.
	// If we set Role=primary here for events that might belong to a subagent,
	// the receiver skips re-classification (it only classifies Unknown roles)
	// and the subagent's subsequent events would be misclassified as primary.
	if name == "session.created" {
		if env.ParentSessionID != "" {
			primaryNativeID = env.ParentSessionID
			role = agentruntime.NativeSessionRoleSubsession
		} else {
			primaryNativeID = env.SessionID
			role = agentruntime.NativeSessionRolePrimary
		}
	}

	id := env.SessionCorrelationID
	if id == "" {
		id = env.SessionID
	}

	tool := normalizeTool(getString(env.Payload, "tool"))

	meta := map[string]string{}
	if env.SessionID != "" {
		meta["session_id"] = env.SessionID
	}
	if env.ParentSessionID != "" {
		meta["parent_session_id"] = env.ParentSessionID
	}
	if v := getString(env.Payload, "callID"); v != "" {
		meta["call_id"] = v
	}
	if v := getString(env.Payload, "permission"); v != "" {
		meta["permission"] = v
	}

	return &agentruntime.Event{
		ID:                id,
		NativeID:          nativeID,
		PrimaryNativeID:   primaryNativeID,
		NativeSessionRole: role,
		Agent:             agentruntime.AgentOpenCode,
		Status:            status,
		Tool:              tool,
		Message:           opencodeMessage(name),
		At:                time.Now(),
		Metadata:          emptyNil(meta),
		Raw: map[string]any{
			"hook_event_name":         env.HookEventName,
			"session_id":              env.SessionID,
			"parent_session_id":       env.ParentSessionID,
			"agentruntime_session_id": env.SessionCorrelationID,
			"payload":                 env.Payload,
		},
	}, nil
}

func opencodeStatus(name string, payload map[string]any) (agentruntime.Status, bool) {
	switch name {
	case "session.created":
		return agentruntime.StatusStarting, true
	case "session.status":
		return agentruntime.StatusWorking, true
	case "tool.execute.before":
		// OpenCode uses the "question" tool for interactive user confirmations
		// (e.g. permission requests). Treat it as awaiting_input on the way in.
		if normalizeTool(getString(payload, "tool")) == "Question" {
			return agentruntime.StatusAwaitingInput, true
		}
		return agentruntime.StatusWorking, true
	case "tool.execute.after":
		return agentruntime.StatusWorking, true
	case "permission.asked":
		return agentruntime.StatusAwaitingInput, true
	case "session.error":
		return agentruntime.StatusError, true
	case "session.idle":
		return agentruntime.StatusIdle, true
	default:
		return "", false
	}
}

func opencodeMessage(name string) string {
	switch name {
	case "session.created":
		return "session started"
	case "session.status":
		return "session status"
	case "session.idle":
		return "turn stopped"
	case "session.error":
		return "session error"
	case "permission.asked":
		return "permission requested"
	case "tool.execute.before":
		return "tool starting"
	case "tool.execute.after":
		return "tool completed"
	default:
		return name
	}
}

func normalizeTool(tool string) string {
	if tool == "" {
		return ""
	}
	switch strings.ToLower(tool) {
	case "bash":
		return "Bash"
	case "read":
		return "Read"
	case "write":
		return "Write"
	case "edit":
		return "Edit"
	case "apply_patch":
		return "ApplyPatch"
	case "question":
		return "Question"
	default:
		return tool
	}
}

func getString(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	v, ok := m[key]
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return s
}

func emptyNil(m map[string]string) map[string]string {
	if len(m) == 0 {
		return nil
	}
	return m
}

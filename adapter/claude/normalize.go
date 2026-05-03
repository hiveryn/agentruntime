package claude

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/hiveryn/agentruntime"
)

var droppedByDesign = map[string]struct{}{
	"InstructionsLoaded": {},
	"ConfigChange":       {},
	"CwdChanged":         {},
	"FileChanged":        {},
	"WorktreeCreate":     {},
	"WorktreeRemove":     {},
	"PreCompact":         {},
	"PostCompact":        {},
	"TaskCreated":        {},
	"TaskCompleted":      {},
	"TeammateIdle":       {},
}

type envelope struct {
	Agent      string            `json:"agent,omitempty"`
	ReceivedAt string            `json:"received_at,omitempty"`
	Hook       map[string]any    `json:"hook,omitempty"`
	Env        map[string]string `json:"env,omitempty"`
	HookCWD    string            `json:"hook_cwd,omitempty"`
	Args       []string          `json:"args,omitempty"`
}

func (a *Adapter) NormalizeEvent(_ context.Context, data []byte) (*agentruntime.Event, error) {
	env, err := decodeEnvelope(data)
	if err != nil {
		return nil, err
	}
	return normalizeEnvelope(env)
}

func decodeEnvelope(data []byte) (envelope, error) {
	var outer map[string]json.RawMessage
	if err := json.Unmarshal(data, &outer); err != nil {
		return envelope{}, err
	}
	if raw, ok := outer["payload"]; ok && raw != nil {
		var env envelope
		if err := json.Unmarshal(raw, &env); err != nil {
			return envelope{}, err
		}
		return env, nil
	}
	var env envelope
	if err := json.Unmarshal(data, &env); err != nil {
		return envelope{}, err
	}
	return env, nil
}

func normalizeEnvelope(env envelope) (*agentruntime.Event, error) {
	if env.Hook == nil {
		return nil, fmt.Errorf("missing hook payload")
	}

	name, _ := env.Hook["hook_event_name"].(string)
	if name == "" {
		return nil, fmt.Errorf("missing hook_event_name")
	}
	if _, drop := droppedByDesign[name]; drop {
		return nil, nil
	}

	status, ok := claudeStatus(name, env.Hook)
	if !ok {
		return nil, nil
	}

	at := time.Now()
	if env.ReceivedAt != "" {
		if parsed, err := time.Parse(time.RFC3339Nano, env.ReceivedAt); err == nil {
			at = parsed
		}
	}

	primaryNativeID, _ := env.Hook["session_id"].(string)
	nativeID := primaryNativeID
	role := agentruntime.NativeSessionRoleUnknown
	if agentID, _ := env.Hook["agent_id"].(string); agentID != "" {
		nativeID = agentID
		role = agentruntime.NativeSessionRoleSubsession
	} else if nativeID != "" {
		role = agentruntime.NativeSessionRolePrimary
	}

	id := env.Env["AGENTRUNTIME_SESSION_ID"]
	if id == "" {
		id = primaryNativeID
		if id == "" {
			id = nativeID
		}
	}

	tool, _ := env.Hook["tool_name"].(string)
	meta := map[string]string{}
	copyHookString(meta, "agent_type", env.Hook)
	copyHookString(meta, "cwd", env.Hook)
	copyHookString(meta, "model", env.Hook)
	copyHookString(meta, "permission_mode", env.Hook)
	copyHookString(meta, "tool_use_id", env.Hook)
	copyHookString(meta, "transcript_path", env.Hook)
	copyHookString(meta, "agent_transcript_path", env.Hook)
	copyHookString(meta, "notification_type", env.Hook)
	copyHookString(meta, "reason", env.Hook)
	copyHookString(meta, "source", env.Hook)
	return &agentruntime.Event{
		ID:                id,
		NativeID:          nativeID,
		PrimaryNativeID:   primaryNativeID,
		NativeSessionRole: role,
		Agent:             agentruntime.AgentClaude,
		Status:            status,
		Tool:              normalizeTool(tool),
		Message:           claudeMessage(name, env.Hook),
		At:                at,
		Metadata:          emptyNil(meta),
		Raw:               rawEnvelope(env),
	}, nil
}

func claudeStatus(name string, raw map[string]any) (agentruntime.Status, bool) {
	switch name {
	case "SessionStart", "SubagentStart":
		return agentruntime.StatusStarting, true
	case "UserPromptSubmit", "PreToolUse", "PostToolUse", "PostToolUseFailure", "PermissionDenied", "ElicitationResult":
		return agentruntime.StatusWorking, true
	case "PermissionRequest", "Elicitation":
		return agentruntime.StatusAwaitingInput, true
	case "Notification":
		switch raw["notification_type"] {
		case "permission_prompt", "elicitation_dialog":
			return agentruntime.StatusAwaitingInput, true
		default:
			return "", false
		}
	case "Stop", "SubagentStop":
		return agentruntime.StatusIdle, true
	case "StopFailure":
		return agentruntime.StatusError, true
	case "SessionEnd":
		return agentruntime.StatusEnded, true
	default:
		return "", false
	}
}

func claudeMessage(name string, raw map[string]any) string {
	switch name {
	case "SessionStart":
		if source, _ := raw["source"].(string); source != "" {
			return "session " + source
		}
		return "session started"
	case "SessionEnd":
		if reason, _ := raw["reason"].(string); reason != "" {
			return "session ended: " + reason
		}
		return "session ended"
	case "UserPromptSubmit":
		return "user prompt submitted"
	case "PreToolUse":
		return "tool starting"
	case "PostToolUse":
		return "tool completed"
	case "PostToolUseFailure":
		return "tool failed"
	case "PermissionRequest":
		return "permission requested"
	case "PermissionDenied":
		return "permission denied"
	case "Notification":
		if kind, _ := raw["notification_type"].(string); kind != "" {
			return "notification: " + kind
		}
		return "notification"
	case "Elicitation":
		return "elicitation requested"
	case "ElicitationResult":
		return "elicitation answered"
	case "SubagentStart":
		return "subagent started"
	case "SubagentStop":
		return "subagent stopped"
	case "Stop":
		return "turn stopped"
	case "StopFailure":
		return "turn failed"
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
	case "apply_patch":
		return "ApplyPatch"
	default:
		return tool
	}
}

func copyHookString(dst map[string]string, key string, src map[string]any) {
	if value, _ := src[key].(string); value != "" {
		dst[key] = value
	}
}

func emptyNil(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	return values
}

func rawEnvelope(env envelope) map[string]any {
	raw := map[string]any{
		"hook": env.Hook,
	}
	if env.Agent != "" {
		raw["agent"] = env.Agent
	}
	if len(env.Env) > 0 {
		raw["env"] = env.Env
	}
	if env.ReceivedAt != "" {
		raw["received_at"] = env.ReceivedAt
	}
	if env.HookCWD != "" {
		raw["hook_cwd"] = env.HookCWD
	}
	if len(env.Args) > 0 {
		raw["args"] = env.Args
	}
	return raw
}

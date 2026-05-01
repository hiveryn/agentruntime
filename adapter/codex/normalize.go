package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/hiveryn/agentruntime"
)

type envelope struct {
	ReceivedAt string            `json:"received_at,omitempty"`
	Hook       map[string]any    `json:"hook"`
	Env        map[string]string `json:"env,omitempty"`
	HookCWD    string            `json:"hook_cwd,omitempty"`
	Args       []string          `json:"args,omitempty"`
}

func (a *Adapter) NormalizeEvent(_ context.Context, data []byte) (*agentruntime.Event, error) {
	var env envelope
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, err
	}
	return normalizeEnvelope(env)
}

func normalizeEnvelope(env envelope) (*agentruntime.Event, error) {
	if env.Hook == nil {
		return nil, fmt.Errorf("missing hook payload")
	}

	name, _ := env.Hook["hook_event_name"].(string)
	if name == "" {
		return nil, fmt.Errorf("missing hook_event_name")
	}

	status, ok := codexStatus(name)
	if !ok {
		return nil, nil
	}

	at := time.Now()
	if env.ReceivedAt != "" {
		if parsed, err := time.Parse(time.RFC3339Nano, env.ReceivedAt); err == nil {
			at = parsed
		}
	}

	nativeID, _ := env.Hook["session_id"].(string)
	id := env.Env["HIVERYN_SESSION_ID"]
	if id == "" {
		id = nativeID
	}

	tool, _ := env.Hook["tool_name"].(string)
	meta := map[string]string{}
	copyHookString(meta, "turn_id", env.Hook)
	copyHookString(meta, "cwd", env.Hook)
	copyHookString(meta, "model", env.Hook)
	copyEnvString(meta, "session_kind", env.Env, "HIVERYN_SESSION_KIND")
	copyEnvString(meta, "architect_folder", env.Env, "HIVERYN_ARCHITECT_FOLDER")
	copyEnvString(meta, "ticket_id", env.Env, "HIVERYN_TICKET_ID")

	return &agentruntime.Event{
		ID:       id,
		NativeID: nativeID,
		Agent:    agentruntime.AgentCodex,
		Status:   status,
		Tool:     normalizeTool(tool),
		Message:  codexMessage(name, env.Hook),
		At:       at,
		Metadata: emptyNil(meta),
		Raw:      rawEnvelope(env),
	}, nil
}

func codexStatus(name string) (agentruntime.Status, bool) {
	switch name {
	case "SessionStart":
		return agentruntime.StatusStarting, true
	case "UserPromptSubmit", "PreToolUse", "PermissionRequest", "PostToolUse":
		return agentruntime.StatusWorking, true
	case "Stop":
		return agentruntime.StatusIdle, true
	default:
		return "", false
	}
}

func codexMessage(name string, raw map[string]any) string {
	switch name {
	case "SessionStart":
		if source, _ := raw["source"].(string); source != "" {
			return "session " + source
		}
		return "session started"
	case "UserPromptSubmit":
		return "user prompt submitted"
	case "PreToolUse":
		return "tool starting"
	case "PermissionRequest":
		return "permission requested"
	case "PostToolUse":
		return "tool completed"
	case "Stop":
		return "turn stopped"
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

func copyEnvString(dst map[string]string, key string, src map[string]string, envKey string) {
	if value := src[envKey]; value != "" {
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

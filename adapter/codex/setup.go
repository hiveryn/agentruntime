package codex

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/hiveryn/agentruntime"
)

const markerField = "agentruntimeMarker"

// managedHookSignature is a stable, agentruntime-branded token present in every
// generated hook command (see hook.go). Codex owns and rewrites hooks.json and
// may drop unknown fields like markerField, so detection must key on the command
// string the agent preserves, not on a custom field it may strip.
const managedHookSignature = "AGENTRUNTIME_SESSION_ID"

var hookEvents = []string{"SessionStart", "UserPromptSubmit", "PreToolUse", "PermissionRequest", "PostToolUse", "Stop"}

func (a *Adapter) EnsureSetup(_ context.Context, req agentruntime.SetupRequest) (agentruntime.SetupResult, error) {
	if req.Marker == "" {
		return agentruntime.SetupResult{}, fmt.Errorf("missing marker")
	}
	if req.Hook.Command == "" {
		return agentruntime.SetupResult{}, fmt.Errorf("missing hook command")
	}

	root, err := codexHome(req.ConfigRoot)
	if err != nil {
		return agentruntime.SetupResult{}, err
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return agentruntime.SetupResult{}, err
	}

	path := filepath.Join(root, "hooks.json")
	state, err := readJSONMap(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			state = map[string]any{}
		} else {
			return agentruntime.SetupResult{}, err
		}
	}
	before, _ := json.Marshal(state)

	installHookCommand(state, req.Marker, req.Hook)

	after, _ := json.MarshalIndent(state, "", "  ")
	if err := os.WriteFile(path, after, 0o600); err != nil {
		return agentruntime.SetupResult{}, err
	}

	return agentruntime.SetupResult{
		Changed: !bytes.Equal(bytes.TrimSpace(before), bytes.TrimSpace(after)),
		Paths:   []string{path},
	}, nil
}

func (a *Adapter) RemoveSetup(_ context.Context, req agentruntime.SetupRequest) (agentruntime.SetupResult, error) {
	if req.Marker == "" {
		return agentruntime.SetupResult{}, fmt.Errorf("missing marker")
	}

	root, err := codexHome(req.ConfigRoot)
	if err != nil {
		return agentruntime.SetupResult{}, err
	}
	path := filepath.Join(root, "hooks.json")
	state, err := readJSONMap(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return agentruntime.SetupResult{Paths: []string{path}}, nil
		}
		return agentruntime.SetupResult{}, err
	}
	before, _ := json.Marshal(state)

	removeHookCommand(state, req.Marker)

	after, _ := json.MarshalIndent(state, "", "  ")
	if err := os.WriteFile(path, after, 0o600); err != nil {
		return agentruntime.SetupResult{}, err
	}

	return agentruntime.SetupResult{
		Changed: !bytes.Equal(bytes.TrimSpace(before), bytes.TrimSpace(after)),
		Paths:   []string{path},
	}, nil
}

func installHookCommand(root map[string]any, marker string, hook agentruntime.HookCommand) {
	timeout := 10
	if hook.Timeout > 0 {
		timeout = int(hook.Timeout / time.Second)
		if timeout < 1 {
			timeout = 1
		}
	}
	statusMessage := hook.StatusMessage
	if statusMessage == "" {
		statusMessage = "agentruntime status capture"
	}

	// Remove every managed hook first, then append exactly one fresh group per
	// targeted event. This collapses any pre-existing duplicates (self-heal) and
	// drops stale-endpoint variants in a single idempotent pass.
	removeHookCommand(root, marker)

	hooks, _ := root["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
		root["hooks"] = hooks
	}

	for _, event := range hookEvents {
		groups, _ := hooks[event].([]any)
		groups = append(groups, map[string]any{
			"matcher": "",
			"hooks": []any{map[string]any{
				"type":          "command",
				"command":       hook.Command,
				"timeout":       timeout,
				"statusMessage": statusMessage,
				markerField:     marker,
			}},
		})
		hooks[event] = groups
	}
}

// isManagedHook reports whether a hook entry belongs to this agentruntime marker.
// It matches the legacy marker field when present, and falls back to the command
// signature when the agent has stripped the field (the duplicate-accumulation bug).
func isManagedHook(hookMap map[string]any, marker string) bool {
	if m, ok := hookMap[markerField].(string); ok && m != "" {
		return m == marker // explicit marker: only ours if it matches
	}
	cmd, _ := hookMap["command"].(string)
	return strings.Contains(cmd, managedHookSignature) // stripped marker: fall back to signature
}

func removeHookCommand(root map[string]any, marker string) {
	hooks, _ := root["hooks"].(map[string]any)
	if hooks == nil {
		return
	}

	for event, raw := range hooks {
		groups, ok := raw.([]any)
		if !ok {
			continue
		}

		keptGroups := make([]any, 0, len(groups))
		for _, group := range groups {
			groupMap, ok := group.(map[string]any)
			if !ok {
				keptGroups = append(keptGroups, group)
				continue
			}
			inner, ok := groupMap["hooks"].([]any)
			if !ok {
				keptGroups = append(keptGroups, group)
				continue
			}

			keptHooks := make([]any, 0, len(inner))
			for _, candidate := range inner {
				hookMap, ok := candidate.(map[string]any)
				if !ok || !isManagedHook(hookMap, marker) {
					keptHooks = append(keptHooks, candidate)
				}
			}
			if len(keptHooks) == 0 {
				continue
			}
			groupMap["hooks"] = keptHooks
			keptGroups = append(keptGroups, groupMap)
		}

		if len(keptGroups) == 0 {
			delete(hooks, event)
			continue
		}
		hooks[event] = keptGroups
	}
}

func readJSONMap(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return map[string]any{}, nil
	}

	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, err
	}
	if root == nil {
		root = map[string]any{}
	}
	return root, nil
}

func codexHome(configRoot string) (string, error) {
	if configRoot != "" {
		return configRoot, nil
	}
	if home := os.Getenv("CODEX_HOME"); home != "" {
		return home, nil
	}
	userHome, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(userHome, ".codex"), nil
}

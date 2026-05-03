package claude

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/hiveryn/agentruntime"
)

const markerField = "agentruntimeMarker"

var hookEvents = []string{
	"SessionStart",
	"SessionEnd",
	"UserPromptSubmit",
	"PreToolUse",
	"PostToolUse",
	"PostToolUseFailure",
	"Stop",
	"StopFailure",
	"Notification",
	"PermissionRequest",
	"PermissionDenied",
	"SubagentStart",
	"SubagentStop",
	"Elicitation",
	"ElicitationResult",
}

func (a *Adapter) EnsureSetup(_ context.Context, req agentruntime.SetupRequest) (agentruntime.SetupResult, error) {
	if req.Marker == "" {
		return agentruntime.SetupResult{}, fmt.Errorf("missing marker")
	}
	if req.Hook.Command == "" {
		return agentruntime.SetupResult{}, fmt.Errorf("missing hook command")
	}

	path, err := settingsPath(req.ConfigRoot)
	if err != nil {
		return agentruntime.SetupResult{}, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return agentruntime.SetupResult{}, err
	}

	state, err := readJSONMap(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return agentruntime.SetupResult{}, err
		}
		state = map[string]any{}
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

	path, err := settingsPath(req.ConfigRoot)
	if err != nil {
		return agentruntime.SetupResult{}, err
	}

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
	hooks, _ := root["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
		root["hooks"] = hooks
	}

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

	for _, event := range hookEvents {
		groups, _ := hooks[event].([]any)
		updated := false
		for _, group := range groups {
			groupMap, _ := group.(map[string]any)
			inner, _ := groupMap["hooks"].([]any)
			for _, candidate := range inner {
				hookMap, _ := candidate.(map[string]any)
				if hookMap[markerField] == marker {
					hookMap["command"] = hook.Command
					hookMap["timeout"] = timeout
					hookMap["statusMessage"] = statusMessage
					updated = true
				}
			}
		}
		if !updated {
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
		}
		hooks[event] = groups
	}
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
				if !ok || hookMap[markerField] != marker {
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
	if len(hooks) == 0 {
		delete(root, "hooks")
	}
}

func settingsPath(configRoot string) (string, error) {
	if configRoot != "" {
		return filepath.Join(configRoot, ".claude", "settings.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude", "settings.json"), nil
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

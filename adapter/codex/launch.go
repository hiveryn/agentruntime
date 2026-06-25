package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/hiveryn/agentruntime"
)

func (a *Adapter) PrepareLaunch(_ context.Context, req agentruntime.StartRequest) (agentruntime.LaunchSpec, error) {
	if req.ID == "" {
		return agentruntime.LaunchSpec{}, fmt.Errorf("missing request ID")
	}
	if req.Workdir == "" {
		return agentruntime.LaunchSpec{}, fmt.Errorf("missing workdir")
	}
	if req.Agent != "" && req.Agent != agentruntime.AgentCodex {
		return agentruntime.LaunchSpec{}, fmt.Errorf("unsupported agent %q", req.Agent)
	}

	command := req.Command
	if command == "" {
		command = "codex"
	}

	args := make([]string, 0, len(req.Args)+10)
	args = append(args, "--enable", "hooks")
	if req.Resume {
		if req.ResumeID != "" {
			args = append(args, "resume", req.ResumeID)
		} else {
			// Bare `codex resume` opens the interactive session picker, matching the
			// id-less resume UX of claude's `--resume`.
			args = append(args, "resume")
		}
	}
	if strings.TrimSpace(req.Instructions) != "" {
		// Keep durable session/role instructions additive by injecting them as a
		// separate developer message instead of replacing Codex base instructions.
		args = append(args, "--config", tomlKV("developer_instructions", req.Instructions))
	}

	for _, server := range req.MCPServers {
		serverArgs, err := mcpConfigArgs(server)
		if err != nil {
			return agentruntime.LaunchSpec{}, err
		}
		args = append(args, serverArgs...)
	}

	args = append(args, req.Args...)
	args = append(args, "--cd", req.Workdir)
	// For bare resume (`resume` picker), codex treats the next positional as
	// SESSION_ID not PROMPT. Only append the prompt when starting fresh or
	// resuming a specific session by ID.
	if req.Prompt != "" && (!req.Resume || req.ResumeID != "") {
		args = append(args, req.Prompt)
	}

	if v, ok := req.Env["AGENTRUNTIME_SESSION_ID"]; ok && v != "" && v != req.ID {
		return agentruntime.LaunchSpec{}, fmt.Errorf("reserved env key AGENTRUNTIME_SESSION_ID is set to %q which conflicts with session ID %q", v, req.ID)
	}

	env := mergeEnv(req.Env, map[string]string{
		"AGENTRUNTIME_SESSION_ID": req.ID,
	})

	return agentruntime.LaunchSpec{
		Command: command,
		Args:    args,
		Env:     env,
		Workdir: req.Workdir,
	}, nil
}

func mcpConfigArgs(server agentruntime.MCPServerConfig) ([]string, error) {
	if server.Name == "" {
		return nil, fmt.Errorf("mcp server missing name")
	}

	prefix := "mcp_servers." + server.Name
	out := []string{}
	add := func(key, value string) {
		out = append(out, "--config", prefix+"."+key+"="+value)
	}

	if server.URL != "" {
		add("url", quoteTOML(server.URL))
		if server.BearerTokenEnvVar != "" {
			add("bearer_token_env_var", quoteTOML(server.BearerTokenEnvVar))
		}
	} else {
		if server.Command == "" {
			return nil, fmt.Errorf("mcp server %q missing command or URL", server.Name)
		}
		add("command", quoteTOML(server.Command))
		if len(server.Args) > 0 {
			add("args", tomlStringArray(server.Args))
		}
		if server.CWD != "" {
			add("cwd", quoteTOML(server.CWD))
		}
		if len(server.Env) > 0 {
			add("env", tomlStringMap(server.Env))
		}
	}

	return out, nil
}

func mergeEnv(left, right map[string]string) map[string]string {
	out := make(map[string]string, len(left)+len(right))
	for key, value := range left {
		out[key] = value
	}
	for key, value := range right {
		out[key] = value
	}
	return out
}

func tomlKV(key, value string) string {
	return key + "=" + quoteTOML(value)
}

func quoteTOML(value string) string {
	data, _ := json.Marshal(value)
	return string(data)
}

func tomlStringArray(values []string) string {
	parts := make([]string, len(values))
	for i, value := range values {
		parts[i] = quoteTOML(value)
	}
	return "[" + strings.Join(parts, ",") + "]"
}

func tomlStringMap(values map[string]string) string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+quoteTOML(values[key]))
	}
	return "{" + strings.Join(parts, ",") + "}"
}

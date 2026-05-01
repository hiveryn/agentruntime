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
	if req.Instructions != "" {
		return agentruntime.LaunchSpec{}, fmt.Errorf("codex instructions transport is not implemented yet")
	}

	command := req.Command
	if command == "" {
		command = "codex"
	}

	args := make([]string, 0, len(req.Args)+8)
	if a.options.NoAltScreen {
		args = append(args, "--no-alt-screen")
	}
	if a.options.EnableHooks {
		args = append(args, "--enable", "codex_hooks")
	}
	if a.options.Sandbox != "" {
		args = append(args, "--sandbox", a.options.Sandbox)
	}
	if a.options.ApprovalPolicy != "" {
		args = append(args, "--config", tomlKV("approval_policy", a.options.ApprovalPolicy))
	}
	if a.options.SkipGitRepoCheck {
		args = append(args, "--skip-git-repo-check")
	}
	if a.options.Model != "" {
		args = append(args, "--model", a.options.Model)
	}
	if a.options.Profile != "" {
		args = append(args, "--profile", a.options.Profile)
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
	if req.Prompt != "" {
		args = append(args, req.Prompt)
	}

	env := mergeEnv(req.Env, map[string]string{
		"HIVERYN_SESSION_ID": req.ID,
	})
	if value := req.Metadata["session_kind"]; value != "" {
		env["HIVERYN_SESSION_KIND"] = value
	}
	if value := req.Metadata["architect_folder"]; value != "" {
		env["HIVERYN_ARCHITECT_FOLDER"] = value
	}
	if value := req.Metadata["ticket_id"]; value != "" {
		env["HIVERYN_TICKET_ID"] = value
	}

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

	if server.DefaultToolsApprovalMode != "" {
		add("default_tools_approval_mode", quoteTOML(server.DefaultToolsApprovalMode))
	}
	if server.ApprovalMode != "" {
		add("approval_mode", quoteTOML(server.ApprovalMode))
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

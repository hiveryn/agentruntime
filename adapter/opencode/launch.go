package opencode

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/hiveryn/agentruntime"
)

type ocConfig struct {
	MCP          map[string]ocMCPServer  `json:"mcp,omitempty"`
	Instructions []string                `json:"instructions,omitempty"`
	Agent        map[string]ocAgentEntry `json:"agent,omitempty"`
	Permission   any                     `json:"permission,omitempty"`
}

type ocAgentEntry struct {
	Description string            `json:"description"`
	Mode        string            `json:"mode"`
	Prompt      string            `json:"prompt,omitempty"`
	Permission  map[string]string `json:"permission,omitempty"`
}

type ocMCPServer struct {
	Type        string            `json:"type"`
	Command     []string          `json:"command,omitempty"`
	Environment map[string]string `json:"environment,omitempty"`
	URL         string            `json:"url,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
	Enabled     bool              `json:"enabled"`
}

var managedArgs = map[string]struct{}{
	"--prompt":   {},
	"--continue": {},
	"-c":         {},
	"--session":  {},
	"-s":         {},
}

func (a *Adapter) PrepareLaunch(_ context.Context, req agentruntime.StartRequest) (agentruntime.LaunchSpec, error) {
	if req.ID == "" {
		return agentruntime.LaunchSpec{}, fmt.Errorf("missing request ID")
	}
	if req.Workdir == "" {
		return agentruntime.LaunchSpec{}, fmt.Errorf("missing workdir")
	}
	if req.Agent != "" && req.Agent != agentruntime.AgentOpenCode {
		return agentruntime.LaunchSpec{}, fmt.Errorf("unsupported agent %q", req.Agent)
	}
	for _, arg := range req.Args {
		if _, ok := managedArgs[arg]; ok {
			return agentruntime.LaunchSpec{}, fmt.Errorf("argument %q is managed by the opencode adapter", arg)
		}
	}

	command := req.Command
	if command == "" {
		command = "opencode"
	}

	cleanupPaths := make([]string, 0)
	cfg := ocConfig{}

	if strings.TrimSpace(req.Instructions) != "" {
		path, err := writeInstructions(req.Instructions)
		if err != nil {
			return agentruntime.LaunchSpec{}, err
		}
		cfg.Instructions = []string{path}
		cleanupPaths = append(cleanupPaths, path)
	}

	if len(req.MCPServers) > 0 {
		cfg.MCP = make(map[string]ocMCPServer, len(req.MCPServers))
		for _, server := range req.MCPServers {
			mapped, err := mapMCPServer(server)
			if err != nil {
				return agentruntime.LaunchSpec{}, err
			}
			cfg.MCP[server.Name] = mapped
		}
	}

	if len(req.OpenCodeAgentConfig) > 0 {
		cfg.Agent = make(map[string]ocAgentEntry, len(req.OpenCodeAgentConfig))
		for name, ac := range req.OpenCodeAgentConfig {
			cfg.Agent[name] = ocAgentEntry{
				Description: ac.Description,
				Mode:        ac.Mode,
				Prompt:      ac.Prompt,
				Permission:  ac.Permission,
			}
		}
	}

	// opencode has no permission-bypass CLI flag; full autonomy is expressed in
	// config. There is no raw arg to conflict with, so nothing to reject here.
	if req.Yolo {
		cfg.Permission = "allow"
	}

	configJSON, err := json.Marshal(cfg)
	if err != nil {
		return agentruntime.LaunchSpec{}, fmt.Errorf("marshal opencode config: %w", err)
	}

	args := make([]string, 0, len(req.Args)+6)
	if req.Resume {
		if req.ResumeID != "" {
			args = append(args, "--session", req.ResumeID)
		} else {
			// opencode has no launch-time session picker (only `--session <id>` or
			// `--continue`). `--continue` (resume the last session) is the closest
			// fallback for an id-less resume.
			args = append(args, "--continue")
		}
	}
	if req.Prompt != "" && (!req.Resume || req.ResumeID == "") {
		args = append(args, "--prompt", req.Prompt)
	}
	if req.Model != "" {
		if a, ok := agentruntime.FindManagedArg(req.Args, "--model", "-m"); ok {
			return agentruntime.LaunchSpec{}, fmt.Errorf("argument %q conflicts with managed model %q; remove it from args", a, req.Model)
		}
		args = append(args, "--model", req.Model)
	}
	// Mode maps to opencode's built-in agents: plan selects the read-only `plan`
	// agent via --agent; build is the default agent (no flag). When mode is set,
	// --agent in raw args is rejected (it also collides with the architect-agent
	// injection the daemon manages).
	plan, err := req.Mode.IsPlan()
	if err != nil {
		return agentruntime.LaunchSpec{}, err
	}
	if req.Mode != "" {
		if a, ok := agentruntime.FindManagedArg(req.Args, "--agent"); ok {
			return agentruntime.LaunchSpec{}, fmt.Errorf("argument %q conflicts with managed mode field; remove it from args", a)
		}
	}
	if plan {
		args = append(args, "--agent", "plan")
	}
	args = append(args, req.Args...)

	if v, ok := req.Env["AGENTRUNTIME_SESSION_ID"]; ok && v != "" && v != req.ID {
		return agentruntime.LaunchSpec{}, fmt.Errorf("reserved env key AGENTRUNTIME_SESSION_ID is set to %q which conflicts with session ID %q", v, req.ID)
	}
	if v, ok := req.Env["OPENCODE_CONFIG_CONTENT"]; ok && v != "" {
		return agentruntime.LaunchSpec{}, fmt.Errorf("reserved env key OPENCODE_CONFIG_CONTENT is managed by the opencode adapter and must not be provided by the caller")
	}

	env := buildEnv(req.Env, req.ID, string(configJSON))

	return agentruntime.LaunchSpec{
		Command:      command,
		Args:         args,
		Env:          env,
		Workdir:      req.Workdir,
		CleanupPaths: cleanupPaths,
	}, nil
}

func writeInstructions(instructions string) (string, error) {
	file, err := os.CreateTemp("", "agentruntime-opencode-instructions-*.md")
	if err != nil {
		return "", fmt.Errorf("create instructions file: %w", err)
	}
	path := file.Name()
	if _, err := file.WriteString(instructions); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return "", fmt.Errorf("write instructions: %w", err)
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return "", fmt.Errorf("close instructions: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = os.Remove(path)
		return "", fmt.Errorf("chmod instructions: %w", err)
	}
	return path, nil
}

func mapMCPServer(server agentruntime.MCPServerConfig) (ocMCPServer, error) {
	if server.Name == "" {
		return ocMCPServer{}, fmt.Errorf("mcp server missing name")
	}
	if server.URL != "" {
		mapped := ocMCPServer{
			Type:    "remote",
			URL:     server.URL,
			Enabled: true,
		}
		if server.BearerTokenEnvVar != "" {
			mapped.Headers = map[string]string{
				"Authorization": "Bearer ${" + server.BearerTokenEnvVar + "}",
			}
		}
		return mapped, nil
	}
	if server.Command == "" {
		return ocMCPServer{}, fmt.Errorf("mcp server %q missing command or URL", server.Name)
	}

	cmd := make([]string, 0, 1+len(server.Args))
	cmd = append(cmd, server.Command)
	cmd = append(cmd, server.Args...)

	mapped := ocMCPServer{
		Type:    "local",
		Command: cmd,
		Enabled: true,
	}
	if len(server.Env) > 0 || server.CWD != "" {
		mapped.Environment = make(map[string]string, len(server.Env)+1)
		for k, v := range server.Env {
			mapped.Environment[k] = v
		}
		if server.CWD != "" {
			mapped.Environment["PWD"] = server.CWD
		}
	}
	return mapped, nil
}

func buildEnv(base map[string]string, id, configJSON string) map[string]string {
	return mergeEnv(base, map[string]string{
		"AGENTRUNTIME_SESSION_ID": id,
		"OPENCODE_CONFIG_CONTENT": configJSON,
	})
}

func mergeEnv(left, right map[string]string) map[string]string {
	out := make(map[string]string, len(left)+len(right))
	for k, v := range left {
		out[k] = v
	}
	for k, v := range right {
		out[k] = v
	}
	return out
}

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
}

type ocAgentEntry struct {
	Description string            `json:"description"`
	Mode        string            `json:"mode"`
	Prompt      string            `json:"prompt,omitempty"`
	Permission  map[string]string `json:"permission"`
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
	"--agent":    {},
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

	configJSON, err := json.Marshal(cfg)
	if err != nil {
		return agentruntime.LaunchSpec{}, fmt.Errorf("marshal opencode config: %w", err)
	}

	args := make([]string, 0, len(req.Args)+6)
	if req.OpenCodeProfile != "" {
		args = append(args, "--agent", req.OpenCodeProfile)
	}
	if req.Resume {
		if req.ResumeID != "" {
			args = append(args, "--session", req.ResumeID)
		} else {
			args = append(args, "--continue")
		}
	}
	if req.Prompt != "" && (!req.Resume || req.ResumeID == "") {
		args = append(args, "--prompt", req.Prompt)
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

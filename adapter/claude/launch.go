package claude

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/hiveryn/agentruntime"
)

type mcpConfig struct {
	MCPServers map[string]mcpServer `json:"mcpServers"`
}

type mcpServer struct {
	Type    string            `json:"type,omitempty"`
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	URL     string            `json:"url,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	// Timeout is the per-server tool-call timeout in milliseconds.
	Timeout int64 `json:"timeout,omitempty"`
}

const (
	// questionTool is claude's native ask-the-user tool.
	questionTool = "AskUserQuestion"
	// autoBackgroundEnv sets when claude moves a pending MCP call to a
	// background task (ending the turn); 0 disables it.
	autoBackgroundEnv = "CLAUDE_CODE_MCP_AUTO_BACKGROUND_MS"
	// autoMemoryEnv disables (1) or enables (0) claude's automatic memory,
	// overriding the autoMemoryEnabled setting.
	autoMemoryEnv = "CLAUDE_CODE_DISABLE_AUTO_MEMORY"
)

var managedArgs = map[string]struct{}{
	"--append-system-prompt": {},
	"--system-prompt":        {},
	"--mcp-config":           {},
	"--session-id":           {},
	"--resume":               {},
	"--add-dir":              {},
}

func (a *Adapter) PrepareLaunch(_ context.Context, req agentruntime.StartRequest) (agentruntime.LaunchSpec, error) {
	if req.ID == "" {
		return agentruntime.LaunchSpec{}, fmt.Errorf("missing request ID")
	}
	if req.Workdir == "" {
		return agentruntime.LaunchSpec{}, fmt.Errorf("missing workdir")
	}
	additionalWorkdirs, err := agentruntime.NormalizeAdditionalWorkdirs(req.Workdir, req.AdditionalWorkdirs)
	if err != nil {
		return agentruntime.LaunchSpec{}, err
	}
	if req.Agent != "" && req.Agent != agentruntime.AgentClaude {
		return agentruntime.LaunchSpec{}, fmt.Errorf("unsupported agent %q", req.Agent)
	}
	for _, arg := range req.Args {
		if _, ok := managedArgs[arg]; ok {
			return agentruntime.LaunchSpec{}, fmt.Errorf("argument %q is managed by the claude adapter", arg)
		}
	}
	headless, err := req.RunMode.IsHeadless()
	if err != nil {
		return agentruntime.LaunchSpec{}, err
	}
	// -p/--print is only managed when headless, so reject it conditionally here
	// rather than in the unconditional managedArgs map.
	if headless {
		if a, ok := agentruntime.FindManagedArg(req.Args, "-p", "--print"); ok {
			return agentruntime.LaunchSpec{}, fmt.Errorf("argument %q conflicts with managed headless run mode; remove it from args", a)
		}
	}

	command := req.Command
	if command == "" {
		command = "claude"
	}

	args := make([]string, 0, len(req.Args)+8+len(additionalWorkdirs))
	cleanupPaths := make([]string, 0, 1)
	// Headless adds --print: claude runs to completion non-interactively and
	// exits. It composes with --permission-mode plan (produces a plan and exits).
	if headless {
		args = append(args, "--print")
	}
	if req.Prompt != "" {
		args = append(args, req.Prompt)
	}
	if req.Model != "" {
		if a, ok := agentruntime.FindManagedArg(req.Args, "--model"); ok {
			return agentruntime.LaunchSpec{}, fmt.Errorf("argument %q conflicts with managed model %q; remove it from args", a, req.Model)
		}
		args = append(args, "--model", req.Model)
	}
	if err := appendClaudePermissionArgs(&args, req); err != nil {
		return agentruntime.LaunchSpec{}, err
	}
	if req.DisableNativeQuestions {
		// The --flag=value form: --disallowedTools is variadic, so a separate
		// value token could swallow a following positional.
		args = append(args, "--disallowedTools="+questionTool)
	}
	if strings.TrimSpace(req.Instructions) != "" {
		flag := "--system-prompt"
		if a.options.AppendInstructions {
			flag = "--append-system-prompt"
		}
		args = append(args, flag, req.Instructions)
	}
	if err := agentruntime.ValidateToolTimeouts(req.MCPServers); err != nil {
		return agentruntime.LaunchSpec{}, err
	}
	if len(req.MCPServers) > 0 {
		path, err := writeMCPConfig(req.MCPServers)
		if err != nil {
			return agentruntime.LaunchSpec{}, err
		}
		args = append(args, "--mcp-config", path)
		cleanupPaths = append(cleanupPaths, path)
	}
	// nativeSessionID is the session handle returned to consumers so they can
	// locate the transcript pre-launch. On resume with an explicit ResumeID it
	// is that id; on resume-most-recent it is unknowable here and left empty.
	var nativeSessionID string
	if req.Resume {
		if req.ResumeID != "" {
			args = append(args, "--resume", req.ResumeID)
			nativeSessionID = req.ResumeID
		} else {
			args = append(args, "--resume")
		}
	} else {
		sessionID, err := a.options.NewSessionID()
		if err != nil {
			return agentruntime.LaunchSpec{}, err
		}
		if sessionID != "" {
			args = append(args, "--session-id", sessionID)
			nativeSessionID = sessionID
		}
	}
	if len(additionalWorkdirs) > 0 {
		args = append(args, "--add-dir")
		args = append(args, additionalWorkdirs...)
	}
	args = append(args, req.Args...)

	sessionEnv, err := agentruntime.SessionEnv(req)
	if err != nil {
		return agentruntime.LaunchSpec{}, err
	}

	env := mergeEnv(req.Env, sessionEnv)
	env = mergeEnv(env, map[string]string{
		// Callers may inherit CLAUDE_CODE_CHILD_SESSION=1 from a parent Claude
		// process (e.g. the daemon itself running under a Claude session).
		// Claude treats that as a signal to disable transcript persistence for
		// its children, which breaks resume after a crash. Force persistence
		// back on for every Hiveryn-launched session, independent of whatever
		// the daemon process happened to inherit.
		"CLAUDE_CODE_FORCE_SESSION_PERSISTENCE": "1",
	})
	// Auto-memory is always set explicitly so the launch never depends on the
	// caller's inherited environment or the user's autoMemoryEnabled setting.
	autoMemory := "1"
	if req.ClaudeAutoMemory {
		autoMemory = "0"
	}
	if v, ok := req.Env[autoMemoryEnv]; ok && v != autoMemory {
		return agentruntime.LaunchSpec{}, fmt.Errorf("env %s=%q conflicts with ClaudeAutoMemory=%t (%s=%s); remove it from env and use ClaudeAutoMemory", autoMemoryEnv, v, req.ClaudeAutoMemory, autoMemoryEnv, autoMemory)
	}
	env[autoMemoryEnv] = autoMemory
	if hasToolTimeout(req.MCPServers) {
		// In the TUI claude moves an MCP call pending for 2 min to a background
		// task and ends the turn; keep long calls in the foreground instead.
		if v, ok := req.Env[autoBackgroundEnv]; ok && v != "0" {
			return agentruntime.LaunchSpec{}, fmt.Errorf("env %s=%q conflicts with mcp tool timeouts, which keep tool calls in the foreground (%s=0); remove it from env", autoBackgroundEnv, v, autoBackgroundEnv)
		}
		env[autoBackgroundEnv] = "0"
	}

	return agentruntime.LaunchSpec{
		Command:            command,
		Args:               args,
		Env:                env,
		Workdir:            req.Workdir,
		AdditionalWorkdirs: additionalWorkdirs,
		CleanupPaths:       cleanupPaths,
		NativeSessionID:    nativeSessionID,
	}, nil
}

// appendClaudePermissionArgs translates the first-class Yolo and Mode fields
// into claude's permission flags, rejecting raw args that conflict with the
// flags it emits. Plan and bypass are both --permission-mode values on claude,
// so combining plan with yolo requires --allow-dangerously-skip-permissions
// (make bypass available) rather than --dangerously-skip-permissions (default on).
func appendClaudePermissionArgs(args *[]string, req agentruntime.StartRequest) error {
	plan, err := req.Mode.IsPlan()
	if err != nil {
		return err
	}
	if !req.Yolo && !plan {
		return nil
	}
	if a, ok := agentruntime.FindManagedArg(req.Args, "--permission-mode", "--dangerously-skip-permissions", "--allow-dangerously-skip-permissions"); ok {
		return fmt.Errorf("argument %q conflicts with managed yolo/mode fields; remove it from args", a)
	}
	switch {
	case req.Yolo && plan:
		*args = append(*args, "--allow-dangerously-skip-permissions", "--permission-mode", "plan")
	case req.Yolo && !plan:
		*args = append(*args, "--dangerously-skip-permissions")
	case !req.Yolo && plan:
		*args = append(*args, "--permission-mode", "plan")
	}
	return nil
}

func hasToolTimeout(servers []agentruntime.MCPServerConfig) bool {
	for _, server := range servers {
		if server.ToolTimeout > 0 {
			return true
		}
	}
	return false
}

func writeMCPConfig(servers []agentruntime.MCPServerConfig) (string, error) {
	config := mcpConfig{MCPServers: make(map[string]mcpServer, len(servers))}
	for _, server := range servers {
		mapped, err := mapMCPServer(server)
		if err != nil {
			return "", err
		}
		config.MCPServers[server.Name] = mapped
	}

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal mcp config: %w", err)
	}

	file, err := os.CreateTemp("", "agentruntime-claude-mcp-*.json")
	if err != nil {
		return "", fmt.Errorf("create mcp config: %w", err)
	}
	path := file.Name()
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return "", fmt.Errorf("write mcp config: %w", err)
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return "", fmt.Errorf("close mcp config: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = os.Remove(path)
		return "", fmt.Errorf("chmod mcp config: %w", err)
	}
	return path, nil
}

func mapMCPServer(server agentruntime.MCPServerConfig) (mcpServer, error) {
	mapped, err := mapMCPTransport(server)
	if err != nil {
		return mcpServer{}, err
	}
	mapped.Timeout = agentruntime.Milliseconds(server.ToolTimeout)
	return mapped, nil
}

func mapMCPTransport(server agentruntime.MCPServerConfig) (mcpServer, error) {
	if server.Name == "" {
		return mcpServer{}, fmt.Errorf("mcp server missing name")
	}
	if server.URL != "" {
		mapped := mcpServer{
			Type: "http",
			URL:  server.URL,
		}
		if server.BearerTokenEnvVar != "" {
			mapped.Headers = map[string]string{
				"Authorization": "Bearer ${" + server.BearerTokenEnvVar + "}",
			}
		}
		return mapped, nil
	}
	if server.Command == "" {
		return mcpServer{}, fmt.Errorf("mcp server %q missing command or URL", server.Name)
	}

	mapped := mcpServer{
		Type:    "stdio",
		Command: server.Command,
		Args:    append([]string(nil), server.Args...),
	}
	if len(server.Env) > 0 {
		mapped.Env = make(map[string]string, len(server.Env))
		for key, value := range server.Env {
			mapped.Env[key] = value
		}
	}
	if server.CWD != "" {
		if mapped.Env == nil {
			mapped.Env = map[string]string{}
		}
		mapped.Env["PWD"] = server.CWD
	}
	return mapped, nil
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

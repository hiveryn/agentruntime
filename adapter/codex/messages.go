package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"

	"github.com/hiveryn/agentruntime"
)

// Message delivery for Codex: the durable thread queue. thread/queue/add
// writes queued_items in $CODEX_HOME/queue_1.sqlite; the process that has the
// thread loaded dispatches the item as its own turn (turn_trigger "queue")
// when the thread is idle, or when `codex resume` loads it. Unlike `codex
// queue`, which mints a fresh client id per call, the caller's MessageID is
// sent as clientUserMessageId and the queue is listed first, so a retry never
// enqueues a second copy while the first is queued. After dispatch the id is
// recorded in the rollout as the user message's client_id, the receipt.
// thread/queue/* are experimental app-server methods (initialize with
// experimentalApi), verified on Codex 0.163.0.

func (a *Adapter) MessageCapability() agentruntime.MessageCapability {
	return agentruntime.MessageCapability{
		Mechanism:  "codex thread queue (app-server thread/queue/add)",
		Mode:       agentruntime.MessageDeliveryPush,
		GateOnIdle: false,
		Coverage: "Queued durably in CODEX_HOME and run as its own turn once the thread is idle, also after resume; " +
			"a busy thread holds it. After an Esc-interrupted turn the queue waits for the user's next completed turn and nothing external can start one. " +
			"Receipt is the rollout user message carrying the id as client_id. " +
			"Dispatch starts the turn before deleting the queue row, so a crash between the two can run the item twice. " +
			"Experimental app-server API, verified on 0.163.0.",
	}
}

// DeliverMessage enqueues the message unless the queue already holds it.
func (a *Adapter) DeliverMessage(ctx context.Context, req agentruntime.DeliverRequest) (agentruntime.DeliverResult, error) {
	if req.NativeSessionID == "" || req.MessageID == "" || strings.TrimSpace(req.Text) == "" {
		return agentruntime.DeliverResult{}, fmt.Errorf("codex: deliver message: thread id, message id and text are required")
	}
	client, err := startAppServer(ctx, req.Command, req.Env)
	if err != nil {
		return agentruntime.DeliverResult{}, err
	}
	defer client.close()
	if err := client.initialize(ctx); err != nil {
		return agentruntime.DeliverResult{}, err
	}
	queued, err := client.queued(ctx, req.NativeSessionID, req.MessageID)
	if err != nil {
		return agentruntime.DeliverResult{}, err
	}
	if queued != "" {
		return agentruntime.DeliverResult{Outcome: agentruntime.DeliverAlreadyPresent, ProviderRef: queued}, nil
	}
	var result struct {
		QueuedSubmission struct {
			ID string `json:"id"`
		} `json:"queuedSubmission"`
	}
	if err := client.call(ctx, "thread/queue/add", map[string]any{
		"threadId":            req.NativeSessionID,
		"clientUserMessageId": req.MessageID,
		"input":               []map[string]any{{"type": "text", "text": req.Text, "text_elements": []any{}}},
	}, &result); err != nil {
		return agentruntime.DeliverResult{}, fmt.Errorf("codex: thread/queue/add: %w", err)
	}
	return agentruntime.DeliverResult{Outcome: agentruntime.DeliverAccepted, ProviderRef: result.QueuedSubmission.ID}, nil
}

// ConfirmMessage reads the rollout for the user message carrying the id as
// client_id and an agent message after it; when it has not appeared and the
// thread is known, it also reports whether the queue still holds it.
func (a *Adapter) ConfirmMessage(ctx context.Context, req agentruntime.ConfirmRequest) (agentruntime.MessageReceipt, error) {
	if req.MessageID == "" {
		return agentruntime.MessageReceipt{}, fmt.Errorf("codex: confirm message: message id is required")
	}
	var receipt agentruntime.MessageReceipt
	if req.TranscriptHandle != "" {
		var err error
		if receipt, err = scanRollout(req.TranscriptHandle, req.MessageID); err != nil {
			return agentruntime.MessageReceipt{}, err
		}
		if receipt.Appeared {
			return receipt, nil
		}
	}
	if req.NativeSessionID == "" {
		return receipt, nil
	}
	client, err := startAppServer(ctx, req.Command, req.Env)
	if err != nil {
		return agentruntime.MessageReceipt{}, err
	}
	defer client.close()
	if err := client.initialize(ctx); err != nil {
		return agentruntime.MessageReceipt{}, err
	}
	queued, err := client.queued(ctx, req.NativeSessionID, req.MessageID)
	if err != nil {
		return agentruntime.MessageReceipt{}, err
	}
	receipt.Queued = queued != ""
	return receipt, nil
}

// scanRollout finds the user message with client_id == messageID and any
// agent message after it. Rollout lines nest the client id either on the
// payload or on payload.item; both are read.
func scanRollout(path, messageID string) (agentruntime.MessageReceipt, error) {
	file, err := os.Open(path)
	if err != nil {
		return agentruntime.MessageReceipt{}, fmt.Errorf("codex: open rollout: %w", err)
	}
	defer func() { _ = file.Close() }()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 1024*1024), 16*1024*1024)
	var receipt agentruntime.MessageReceipt
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var rec struct {
			Payload struct {
				Type     string `json:"type"`
				ClientID string `json:"client_id"`
				Item     struct {
					Type     string `json:"type"`
					ClientID string `json:"client_id"`
				} `json:"item"`
			} `json:"payload"`
		}
		if err := json.Unmarshal(line, &rec); err != nil {
			continue
		}
		kind := rec.Payload.Type
		if rec.Payload.Item.Type != "" {
			kind = rec.Payload.Item.Type
		}
		switch {
		case !receipt.Appeared && (rec.Payload.ClientID == messageID || rec.Payload.Item.ClientID == messageID):
			receipt.Appeared = true
		case receipt.Appeared && isAgentMessageKind(kind):
			receipt.Acknowledged = true
		}
	}
	if err := scanner.Err(); err != nil {
		return agentruntime.MessageReceipt{}, fmt.Errorf("codex: scan rollout: %w", err)
	}
	return receipt, nil
}

// isAgentMessageKind matches the agent reply item in both spellings the
// rollout uses: event_msg payload type "agent_message" and item_completed item
// type "AgentMessage" (0.163.0).
func isAgentMessageKind(kind string) bool {
	return strings.EqualFold(strings.ReplaceAll(kind, "_", ""), "agentmessage")
}

// appServerClient speaks JSON-RPC over the stdio of an embedded
// `codex app-server`, which shares the durable queue with any running TUI or
// shared daemon.
type appServerClient struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
	stderr strings.Builder
	mu     sync.Mutex
	next   int
}

type rpcResponse struct {
	ID     *int            `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func startAppServer(ctx context.Context, command string, env map[string]string) (*appServerClient, error) {
	if command == "" {
		command = "codex"
	}
	cmd := exec.CommandContext(ctx, command, "app-server")
	cmd.Env = mergeOSEnv(env)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("codex app-server stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("codex app-server stdout: %w", err)
	}
	client := &appServerClient{cmd: cmd, stdin: stdin, stdout: bufio.NewReaderSize(stdout, 1024*1024)}
	cmd.Stderr = &client.stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start codex app-server: %w", err)
	}
	return client, nil
}

func (c *appServerClient) close() {
	_ = c.stdin.Close()
	_ = c.cmd.Process.Kill()
	_ = c.cmd.Wait()
}

func (c *appServerClient) initialize(ctx context.Context) error {
	if err := c.call(ctx, "initialize", map[string]any{
		"clientInfo":   map[string]any{"name": "agentruntime", "version": "0"},
		"capabilities": map[string]any{"experimentalApi": true},
	}, nil); err != nil {
		return fmt.Errorf("codex app-server initialize: %w", err)
	}
	return c.notify("initialized")
}

// queued returns the queued submission id carrying clientUserMessageId, or "".
func (c *appServerClient) queued(ctx context.Context, threadID, clientMessageID string) (string, error) {
	var cursor any
	for {
		params := map[string]any{"threadId": threadID}
		if cursor != nil {
			params["cursor"] = cursor
		}
		var page struct {
			Data []struct {
				ID                  string `json:"id"`
				ClientUserMessageID string `json:"clientUserMessageId"`
			} `json:"data"`
			NextCursor any `json:"nextCursor"`
		}
		if err := c.call(ctx, "thread/queue/list", params, &page); err != nil {
			return "", fmt.Errorf("codex: thread/queue/list: %w", err)
		}
		for _, item := range page.Data {
			if item.ClientUserMessageID == clientMessageID {
				return item.ID, nil
			}
		}
		if page.NextCursor == nil || page.NextCursor == "" {
			return "", nil
		}
		cursor = page.NextCursor
	}
}

func (c *appServerClient) notify(method string) error {
	data, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": method})
	if _, err := c.stdin.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("codex app-server write: %w", err)
	}
	return nil
}

// call sends one request and waits for its response, skipping notifications
// the server emits meanwhile.
func (c *appServerClient) call(ctx context.Context, method string, params any, result any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.next++
	id := c.next
	data, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	if err != nil {
		return err
	}
	if _, err := c.stdin.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("codex app-server write: %w", err)
	}
	type lineResult struct {
		line []byte
		err  error
	}
	for {
		ch := make(chan lineResult, 1)
		go func() {
			line, err := c.stdout.ReadBytes('\n')
			ch <- lineResult{line, err}
		}()
		var lr lineResult
		select {
		case <-ctx.Done():
			return ctx.Err()
		case lr = <-ch:
		}
		if lr.err != nil {
			if errors.Is(lr.err, io.EOF) {
				return fmt.Errorf("codex app-server exited before answering %s: %s", method, strings.TrimSpace(c.stderr.String()))
			}
			return fmt.Errorf("codex app-server read: %w", lr.err)
		}
		var resp rpcResponse
		if err := json.Unmarshal(lr.line, &resp); err != nil || resp.ID == nil || *resp.ID != id {
			continue
		}
		if resp.Error != nil {
			return fmt.Errorf("%s (code %d)", resp.Error.Message, resp.Error.Code)
		}
		if result != nil && len(resp.Result) > 0 {
			if err := json.Unmarshal(resp.Result, result); err != nil {
				return fmt.Errorf("decode %s result: %w", method, err)
			}
		}
		return nil
	}
}

// mergeOSEnv layers the session environment over the process environment so
// the embedded app-server resolves the same CODEX_HOME as the session.
func mergeOSEnv(env map[string]string) []string {
	merged := map[string]string{}
	for _, kv := range os.Environ() {
		if k, v, ok := strings.Cut(kv, "="); ok {
			merged[k] = v
		}
	}
	for k, v := range env {
		merged[k] = v
	}
	out := make([]string, 0, len(merged))
	for k, v := range merged {
		out = append(out, k+"="+v)
	}
	return out
}

package codex

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/hiveryn/agentruntime"
	"github.com/hiveryn/agentruntime/internal/chatwire"
)

func (a *Adapter) PrepareConversation(ctx context.Context, req agentruntime.StartRequest) (agentruntime.ConversationSpec, error) {
	if req.Resume && req.ResumeID == "" {
		return agentruntime.ConversationSpec{}, fmt.Errorf("codex chat resume requires native thread ID")
	}
	// Only app-server-compatible configuration flags can be forwarded. Reject
	// unknown flags rather than quietly losing configured policy or settings.
	raw := []string{}
	for i := 0; i < len(req.Args); i++ {
		switch req.Args[i] {
		case "-c", "--config", "--enable", "--disable":
			if i+1 >= len(req.Args) {
				return agentruntime.ConversationSpec{}, fmt.Errorf("missing value for %s", req.Args[i])
			}
			raw = append(raw, req.Args[i], req.Args[i+1])
			i++
		default:
			return agentruntime.ConversationSpec{}, fmt.Errorf("codex chat does not support profile argument %q; use app-server config overrides", req.Args[i])
		}
	}
	original := req
	req.Args = raw
	req.Prompt = ""
	req.Resume = false
	req.ResumeID = ""
	req.MessageDelivery = nil
	req.RunMode = agentruntime.RunInteractive
	spec, err := a.PrepareLaunch(ctx, req)
	if err != nil {
		return agentruntime.ConversationSpec{}, err
	}
	args := []string{"app-server"}
	for i := 0; i < len(spec.Args); i++ {
		switch spec.Args[i] {
		case "--config", "-c", "--enable", "--disable":
			args = append(args, spec.Args[i], spec.Args[i+1])
			i++
		case "--model", "--cd", "--add-dir":
			i++
		case "--dangerously-bypass-hook-trust", "--dangerously-bypass-approvals-and-sandbox":
		default:
			return agentruntime.ConversationSpec{}, fmt.Errorf("unsupported synthesized codex chat argument %q", spec.Args[i])
		}
	}
	spec.Args = args
	return agentruntime.ConversationSpec{Launch: spec, Connect: func(ctx context.Context, r io.Reader, w io.Writer) (agentruntime.Conversation, error) {
		wire := chatwire.New(ctx, r, w)
		if _, err := wire.Call(ctx, "initialize", chatwire.Object{"clientInfo": chatwire.Object{"name": "hiveryn", "version": "1"}, "capabilities": chatwire.Object{"experimentalApi": true}}, nil); err != nil {
			return nil, err
		}
		params := chatwire.Object{"cwd": original.Workdir, "model": original.Model}
		if original.Yolo {
			params["approvalPolicy"] = "never"
			params["sandbox"] = "danger-full-access"
		}
		// Additional writable directories use the same native config key as the CLI.
		if len(original.AdditionalWorkdirs) > 0 && !original.Yolo {
			params["config"] = chatwire.Object{"sandbox_workspace_write.writable_roots": original.AdditionalWorkdirs}
		}
		method := "thread/start"
		if original.Resume {
			method = "thread/resume"
			params["threadId"] = original.ResumeID
		}
		result, err := wire.Call(ctx, method, params, nil)
		if err != nil {
			return nil, err
		}
		id := chatwire.String(chatwire.Map(result, "thread"), "id")
		if id == "" {
			return nil, fmt.Errorf("%s returned no thread ID", method)
		}
		return &codexConversation{wire: wire, id: id}, nil
	}}, nil
}

type codexConversation struct {
	wire *chatwire.Wire
	id   string
}

func (c *codexConversation) NativeID() string { return c.id }
func (c *codexConversation) Turn(ctx context.Context, in agentruntime.ConversationInput, observe func(agentruntime.ConversationEvent) error) error {
	// turn/start answering with a turn id, or any item of this thread's turn
	// arriving before that answer, is the app-server running this input.
	observe = chatwire.AcceptOnFirst(observe, "codex_turn_start")
	var tid string
	onEvent := func(m chatwire.Object) error {
		p := chatwire.Map(m, "params")
		method := chatwire.String(m, "method")
		if m["id"] != nil && method != "" {
			return observe(agentruntime.ConversationEvent{ID: fmt.Sprintf("permission:%v", m["id"]), TurnID: chatwire.String(p, "turnId"), Role: "system", Status: "blocked", Text: "Native request pending: " + method + "; native permission control is unavailable in chat V1", At: time.Now().UTC()})
		}
		if method != "item/completed" && method != "item/started" {
			return nil
		}
		if chatwire.String(p, "threadId") != c.id {
			return nil
		}
		item := chatwire.Map(p, "item")
		e := agentruntime.ConversationEvent{ID: chatwire.String(item, "id"), TurnID: chatwire.String(p, "turnId"), At: time.Now().UTC(), Status: "completed"}
		if method == "item/started" {
			e.Status = "started"
		}
		switch chatwire.String(item, "type") {
		case "agentMessage":
			e.Role = "assistant"
			e.Text = chatwire.String(item, "text")
		case "userMessage":
			e.Role = "user"
			for _, v := range chatwire.List(item, "content") {
				p, ok := v.(map[string]any)
				if ok && chatwire.String(p, "type") == "text" {
					e.Text += chatwire.String(p, "text")
				}
			}
		case "commandExecution", "mcpToolCall", "fileChange", "webSearch":
			e.Role = "tool"
			e.Tool = chatwire.String(item, "type")
			e.Text = chatwire.String(item, "aggregatedOutput")
			if e.Tool == "mcpToolCall" {
				if name := chatwire.String(item, "tool"); name != "" {
					e.Tool = chatwire.String(item, "server") + "/" + name
				}
				e.Text = chatwire.TextContent(chatwire.Map(item, "result")["content"])
				if message := chatwire.String(chatwire.Map(item, "error"), "message"); message != "" {
					e.Text = message
				}
			}
			if status := chatwire.String(item, "status"); status != "" {
				e.Status = status
			}
		default:
			return nil
		}
		return observe(e)
	}
	result, err := c.wire.Call(ctx, "turn/start", chatwire.Object{"threadId": c.id, "input": []any{chatwire.Object{"type": "text", "text": in.Text}}, "clientUserMessageId": in.ID}, onEvent)
	if err != nil {
		return err
	}
	tid = chatwire.String(chatwire.Map(result, "turn"), "id")
	if tid == "" {
		return fmt.Errorf("turn/start returned no turn ID")
	}
	if err := observe(agentruntime.ConversationEvent{ID: "submission:" + in.ID, TurnID: tid, Role: "system", Status: "started", At: time.Now().UTC()}); err != nil {
		return err
	}
	for {
		m, err := c.wire.Read(ctx)
		if err != nil {
			return err
		}
		p := chatwire.Map(m, "params")
		turn := chatwire.Map(p, "turn")
		if chatwire.String(m, "method") == "turn/completed" && chatwire.String(p, "threadId") == c.id && chatwire.String(turn, "id") == tid {
			e := agentruntime.ConversationEvent{ID: "turn:" + tid, TurnID: tid, Role: "system", Status: chatwire.String(turn, "status"), Complete: true, At: time.Now().UTC()}
			if e.Status != "completed" {
				e.Status = "failed"
				e.Text = fmt.Sprint(turn["error"])
			}
			return observe(e)
		}
		if err := onEvent(m); err != nil {
			return err
		}
	}
}

func (c *codexConversation) Reconcile(ctx context.Context, _ agentruntime.ConversationInput, tid string) ([]agentruntime.ConversationEvent, error) {
	if tid == "" {
		return nil, nil
	}
	result, err := c.wire.Call(ctx, "thread/read", chatwire.Object{"threadId": c.id, "includeTurns": true}, nil)
	if err != nil {
		return nil, err
	}
	for _, v := range chatwire.List(chatwire.Map(result, "thread"), "turns") {
		turn, ok := v.(map[string]any)
		if !ok || chatwire.String(turn, "id") != tid {
			continue
		}
		status := chatwire.String(turn, "status")
		if status != "completed" && status != "failed" && status != "interrupted" {
			return nil, nil
		}
		events := []agentruntime.ConversationEvent{}
		for _, v := range chatwire.List(turn, "items") {
			item, ok := v.(map[string]any)
			if ok && chatwire.String(item, "type") == "agentMessage" {
				events = append(events, agentruntime.ConversationEvent{ID: chatwire.String(item, "id"), TurnID: tid, Role: "assistant", Text: chatwire.String(item, "text"), Status: "completed"})
			}
		}
		e := agentruntime.ConversationEvent{ID: "turn:" + tid, TurnID: tid, Role: "system", Status: status, Complete: true}
		if status != "completed" {
			e.Status = "failed"
			e.Text = fmt.Sprint(turn["error"])
		}
		return append(events, e), nil
	}
	return nil, nil
}

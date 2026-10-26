package claude

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/hiveryn/agentruntime"
	"github.com/hiveryn/agentruntime/internal/chatwire"
)

func (a *Adapter) PrepareConversation(ctx context.Context, req agentruntime.StartRequest) (agentruntime.ConversationSpec, error) {
	if req.Resume && req.ResumeID == "" {
		return agentruntime.ConversationSpec{}, fmt.Errorf("claude chat resume requires a native session ID")
	}
	if arg, ok := agentruntime.FindManagedArg(req.Args, "--input-format", "--output-format", "--permission-prompt-tool", "--replay-user-messages", "--verbose"); ok {
		return agentruntime.ConversationSpec{}, fmt.Errorf("chat manages argument %q", arg)
	}
	req.Prompt = ""
	req.MessageDelivery = nil
	req.RunMode = agentruntime.RunHeadless
	spec, err := a.PrepareLaunch(ctx, req)
	if err != nil {
		return agentruntime.ConversationSpec{}, err
	}
	spec.Args = append(spec.Args, "--input-format", "stream-json", "--output-format", "stream-json", "--verbose", "--replay-user-messages", "--permission-prompt-tool", "stdio")
	return agentruntime.ConversationSpec{Launch: spec, Connect: func(ctx context.Context, r io.Reader, w io.Writer) (agentruntime.Conversation, error) {
		wire := chatwire.New(ctx, r, w)
		if err := wire.Send(chatwire.Object{"type": "control_request", "request_id": "initialize", "request": chatwire.Object{"subtype": "initialize"}}); err != nil {
			return nil, err
		}
		for {
			m, err := wire.Read(ctx)
			if err != nil {
				return nil, err
			}
			if chatwire.String(m, "type") == "control_response" {
				response := chatwire.Map(m, "response")
				if chatwire.String(response, "subtype") == "error" {
					return nil, fmt.Errorf("claude initialize: %v", response["error"])
				}
				return &claudeConversation{wire: wire, id: spec.NativeSessionID, adapter: a, configRoot: a.ConfigRoot(req.Env)}, nil
			}
		}
	}}, nil
}

type claudeConversation struct {
	adapter    *Adapter
	configRoot string
	wire       *chatwire.Wire
	id         string
}

func (c *claudeConversation) NativeID() string { return c.id }
func (c *claudeConversation) Turn(ctx context.Context, in agentruntime.ConversationInput, observe func(agentruntime.ConversationEvent) error) error {
	m := chatwire.Object{"type": "user", "uuid": in.ID, "session_id": c.id, "message": chatwire.Object{"role": "user", "content": in.Text}, "parent_tool_use_id": nil}
	if err := c.wire.Send(m); err != nil {
		return err
	}
	for {
		m, err := c.wire.Read(ctx)
		if err != nil {
			return err
		}
		typ := chatwire.String(m, "type")
		event := agentruntime.ConversationEvent{TurnID: in.ID, At: time.Now().UTC()}
		switch typ {
		case "control_request":
			event.ID = "permission:" + chatwire.String(m, "request_id")
			event.Role = "system"
			event.Status = "blocked"
			event.Text = "Native permission request pending; native permission control is unavailable in chat V1"
			event.Tool = chatwire.String(chatwire.Map(m, "request"), "tool_name")
		case "assistant", "user":
			event.ID = chatwire.String(m, "uuid")
			event.Role = typ
			event.Status = "completed"
			for i, raw := range chatwire.List(chatwire.Map(m, "message"), "content") {
				part, ok := raw.(map[string]any)
				if !ok {
					continue
				}
				e := event
				e.ID = fmt.Sprintf("%s:%d", event.ID, i)
				switch chatwire.String(part, "type") {
				case "text":
					e.Text = chatwire.String(part, "text")
				case "tool_use":
					e.Role = "tool"
					e.Tool = chatwire.String(part, "name")
					e.ID = chatwire.String(part, "id")
					e.Status = "started"
				case "tool_result":
					e.Role = "tool"
					e.ID = chatwire.String(part, "tool_use_id")
					e.Status = "completed"
					if part["is_error"] == true {
						e.Status = "failed"
					}
					e.Text = chatwire.TextContent(part["content"])
				default:
					continue
				}
				if err := observe(e); err != nil {
					return err
				}
			}
			continue
		case "result":
			matches := chatwire.String(m, "user_message_uuid") == in.ID
			for _, id := range chatwire.List(m, "user_message_uuids") {
				if id == in.ID {
					matches = true
				}
			}
			if !matches {
				return fmt.Errorf("claude result has no matching user_message_uuid for %s", in.ID)
			}
			event.ID = "result:" + chatwire.String(m, "uuid")
			event.Role = "system"
			event.Status = "completed"
			event.Complete = true
			if m["is_error"] == true {
				event.Status = "failed"
				event.Text = chatwire.String(m, "result")
				if event.Text == "" {
					event.Text = fmt.Sprint(m["errors"])
				}
			}
		default:
			continue
		}
		if err := observe(event); err != nil {
			return err
		}
		if event.Complete {
			return nil
		}
	}
}

func (c *claudeConversation) Reconcile(ctx context.Context, in agentruntime.ConversationInput, _ string) ([]agentruntime.ConversationEvent, error) {
	path, err := c.adapter.LocateTranscript(ctx, agentruntime.LocateRequest{NativeSessionID: c.id, ConfigRoot: c.configRoot})
	if err != nil {
		return nil, err
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 4096), 16<<20)
	lineage := map[string]bool{in.ID: true}
	var events []agentruntime.ConversationEvent
	for scanner.Scan() {
		var m chatwire.Object
		if err = json.Unmarshal(scanner.Bytes(), &m); err != nil {
			return nil, err
		}
		id := chatwire.String(m, "uuid")
		if !lineage[chatwire.String(m, "parentUuid")] {
			continue
		}
		// A subsequent SDK/human user turn starts another lineage. Tool-result
		// user records have no turnOrigin and remain part of this turn.
		if chatwire.String(m, "type") == "user" && chatwire.String(m, "turnOrigin") != "" {
			continue
		}
		lineage[id] = true
		if chatwire.String(m, "type") != "assistant" {
			continue
		}
		msg := chatwire.Map(m, "message")
		at, _ := time.Parse(time.RFC3339Nano, chatwire.String(m, "timestamp"))
		for i, v := range chatwire.List(msg, "content") {
			part, ok := v.(map[string]any)
			if ok && chatwire.String(part, "type") == "text" {
				events = append(events, agentruntime.ConversationEvent{ID: fmt.Sprintf("%s:%d", id, i), TurnID: in.ID, Role: "assistant", Text: chatwire.String(part, "text"), Status: "completed", At: at})
			}
		}
		if chatwire.String(msg, "stop_reason") == "end_turn" || chatwire.String(msg, "stop_reason") == "max_tokens" {
			events = append(events, agentruntime.ConversationEvent{ID: "recovered:" + in.ID, TurnID: in.ID, Role: "system", Status: "completed", Complete: true, At: at})
			return events, nil
		}
	}
	return nil, scanner.Err()
}

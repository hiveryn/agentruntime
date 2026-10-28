package opencode

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/hiveryn/agentruntime"
	"github.com/hiveryn/agentruntime/internal/chatwire"
)

func (a *Adapter) PrepareConversation(ctx context.Context, req agentruntime.StartRequest) (agentruntime.ConversationSpec, error) {
	if req.Resume && req.ResumeID == "" {
		return agentruntime.ConversationSpec{}, fmt.Errorf("opencode chat resume requires native session ID")
	}
	if req.MessageDelivery == nil {
		return agentruntime.ConversationSpec{}, fmt.Errorf("opencode chat requires private server address")
	}
	agent := ""
	for i := 0; i < len(req.Args); i++ {
		if req.Args[i] == "--agent" && i+1 < len(req.Args) {
			agent = req.Args[i+1]
			i++
		} else {
			return agentruntime.ConversationSpec{}, fmt.Errorf("opencode chat does not support profile argument %q", req.Args[i])
		}
	}
	if req.Mode == agentruntime.ModePlan {
		agent = "plan"
	}
	original := req
	req.Prompt = ""
	req.Resume = false
	req.ResumeID = ""
	req.RunMode = agentruntime.RunInteractive
	spec, err := a.PrepareLaunch(ctx, req)
	if err != nil {
		return agentruntime.ConversationSpec{}, err
	}
	spec.Args = []string{"serve", "--hostname", "127.0.0.1", "--port", fmt.Sprint(req.MessageDelivery.ServerPort)}
	return agentruntime.ConversationSpec{Launch: spec, Connect: func(ctx context.Context, _ io.Reader, _ io.Writer) (agentruntime.Conversation, error) {
		c := &ocConversation{base: fmt.Sprintf("http://127.0.0.1:%d", original.MessageDelivery.ServerPort), password: original.MessageDelivery.ServerPassword, model: original.Model, agent: agent, client: &http.Client{Timeout: 10 * time.Second}}
		for {
			_, err := c.call(ctx, "GET", "/session", nil)
			if err == nil {
				break
			}
			select {
			case <-ctx.Done():
				return nil, fmt.Errorf("opencode readiness: %w: %v", ctx.Err(), err)
			case <-time.After(100 * time.Millisecond):
			}
		}
		path := "/session"
		method := "POST"
		var body any = chatwire.Object{}
		if original.Resume {
			path += "/" + original.ResumeID
			method = "GET"
			body = nil
		}
		raw, err := c.call(ctx, method, path, body)
		if err != nil {
			return nil, err
		}
		var m chatwire.Object
		if err = json.Unmarshal(raw, &m); err != nil {
			return nil, err
		}
		c.id = chatwire.String(m, "id")
		if c.id == "" {
			return nil, fmt.Errorf("opencode returned no session ID")
		}
		return c, nil
	}}, nil
}

type ocConversation struct {
	base, password, id, model, agent string
	client                           *http.Client
}

func (c *ocConversation) NativeID() string { return c.id }
func (c *ocConversation) call(ctx context.Context, method, path string, body any) ([]byte, error) {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, reader)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth("opencode", c.password)
	req.Header.Set("Content-Type", "application/json")
	res, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = res.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(res.Body, 16<<20))
	if err != nil {
		return nil, err
	}
	if res.StatusCode >= 300 {
		return nil, fmt.Errorf("opencode %s %s: HTTP %d: %s", method, path, res.StatusCode, data)
	}
	return data, nil
}
func (c *ocConversation) Turn(ctx context.Context, in agentruntime.ConversationInput, observe func(agentruntime.ConversationEvent) error) error {
	eventsCtx, cancelEvents := context.WithCancel(ctx)
	defer cancelEvents()
	nativeErrors, err := c.watchErrors(eventsCtx)
	if err != nil {
		return err
	}

	// UUID entropy combined with OpenCode's ascending 48-bit timestamp prefix.
	mid := fmt.Sprintf("msg_%012x%s", (time.Now().UnixMilli()*4096)&((1<<48)-1), strings.ReplaceAll(in.ID, "-", "")[:14])
	body := chatwire.Object{"messageID": mid, "parts": []any{chatwire.Object{"type": "text", "text": in.Text}}}
	if c.agent != "" {
		body["agent"] = c.agent
	}
	if c.model != "" {
		p := strings.SplitN(c.model, "/", 2)
		if len(p) != 2 {
			return fmt.Errorf("opencode model must be provider/model, got %q", c.model)
		}
		body["model"] = chatwire.Object{"providerID": p[0], "modelID": p[1]}
	}
	if _, err := c.call(ctx, "POST", "/session/"+c.id+"/prompt_async", body); err != nil {
		return err
	}
	// A 2xx from prompt_async is the server accepting the input; completion
	// is the assistant message finishing below.
	if err := observe(agentruntime.ConversationEvent{ID: in.ID, TurnID: mid, Role: "user", Text: in.Text, Status: "started", At: time.Now().UTC(), Acceptance: "opencode_prompt_async"}); err != nil {
		return err
	}
	seen := map[string]string{}
	for {
		select {
		case nativeErr := <-nativeErrors:
			return nativeErr
		default:
		}
		raw, err := c.call(ctx, "GET", "/session/"+c.id+"/message", nil)
		if err != nil {
			return err
		}
		var messages []chatwire.Object
		if err = json.Unmarshal(raw, &messages); err != nil {
			return err
		}
		complete := false
		status := "completed"
		failure := ""
		for _, m := range messages {
			info := chatwire.Map(m, "info")
			if chatwire.String(info, "parentID") != mid {
				continue
			}
			if info["error"] != nil {
				complete = true
				status = "failed"
				b, _ := json.Marshal(info["error"])
				failure = string(b)
			}
			if chatwire.String(info, "finish") == "stop" && chatwire.Map(info, "time")["completed"] != nil {
				complete = true
			}
			for _, v := range chatwire.List(m, "parts") {
				p, ok := v.(map[string]any)
				if !ok {
					continue
				}
				e := agentruntime.ConversationEvent{ID: chatwire.String(p, "id"), TurnID: mid, Role: "assistant", Status: "completed", At: time.Now().UTC()}
				switch chatwire.String(p, "type") {
				case "text":
					e.Text = chatwire.String(p, "text")
				case "tool":
					e.Role = "tool"
					e.Tool = chatwire.String(p, "tool")
					state := chatwire.Map(p, "state")
					e.Status = chatwire.String(state, "status")
					e.Text = chatwire.String(state, "output")
					if e.Status == "error" {
						e.Status = "failed"
						e.Text = chatwire.String(state, "error")
					}
				default:
					continue
				}
				key := e.Status + "\x00" + e.Text
				if seen[e.ID] == key {
					continue
				}
				if err := observe(e); err != nil {
					return err
				}
				seen[e.ID] = key
			}
		}
		if complete {
			return observe(agentruntime.ConversationEvent{ID: "turn:" + mid, TurnID: mid, Role: "system", Status: status, Text: failure, Complete: true, At: time.Now().UTC()})
		}
		raw, err = c.call(ctx, "GET", "/permission", nil)
		if err != nil {
			return err
		}
		var permissions []chatwire.Object
		if err = json.Unmarshal(raw, &permissions); err != nil {
			return err
		}
		for _, p := range permissions {
			if chatwire.String(p, "sessionID") == c.id {
				e := agentruntime.ConversationEvent{ID: "permission:" + chatwire.String(p, "id"), TurnID: mid, Role: "system", Status: "blocked", Text: "Native permission pending: " + chatwire.String(p, "permission") + "; native permission control is unavailable in chat V1", At: time.Now().UTC()}
				if seen[e.ID] != "blocked" {
					if err := observe(e); err != nil {
						return err
					}
					seen[e.ID] = "blocked"
				}
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
}

func (c *ocConversation) Reconcile(ctx context.Context, _ agentruntime.ConversationInput, mid string) ([]agentruntime.ConversationEvent, error) {
	if mid == "" {
		return nil, nil
	}
	raw, err := c.call(ctx, "GET", "/session/"+c.id+"/message", nil)
	if err != nil {
		return nil, err
	}
	var messages []chatwire.Object
	if err = json.Unmarshal(raw, &messages); err != nil {
		return nil, err
	}
	events := []agentruntime.ConversationEvent{}
	for _, m := range messages {
		info := chatwire.Map(m, "info")
		if chatwire.String(info, "parentID") != mid {
			continue
		}
		for _, v := range chatwire.List(m, "parts") {
			part, ok := v.(map[string]any)
			if ok && chatwire.String(part, "type") == "text" {
				events = append(events, agentruntime.ConversationEvent{ID: chatwire.String(part, "id"), TurnID: mid, Role: "assistant", Text: chatwire.String(part, "text"), Status: "completed"})
			}
		}
		if info["error"] != nil {
			data, _ := json.Marshal(info["error"])
			return append(events, agentruntime.ConversationEvent{ID: "turn:" + mid, TurnID: mid, Role: "system", Status: "failed", Text: string(data), Complete: true}), nil
		}
		if chatwire.String(info, "finish") == "stop" && chatwire.Map(info, "time")["completed"] != nil {
			return append(events, agentruntime.ConversationEvent{ID: "turn:" + mid, TurnID: mid, Role: "system", Status: "completed", Complete: true}), nil
		}
	}
	return nil, nil
}

// Subscribe before handoff: prompt_async can accept a message (204) and then
// report a catalog/auth failure only through session.error, with no reply row.
func (c *ocConversation) watchErrors(ctx context.Context) (<-chan error, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.base+"/event", nil)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth("opencode", c.password)
	response, err := (&http.Client{}).Do(req)
	if err != nil {
		return nil, err
	}
	if response.StatusCode != 200 {
		_ = response.Body.Close()
		return nil, fmt.Errorf("opencode event stream HTTP %d", response.StatusCode)
	}
	failures := make(chan error, 1)
	go func() {
		defer func() { _ = response.Body.Close() }()
		scanner := bufio.NewScanner(response.Body)
		scanner.Buffer(make([]byte, 4096), 16<<20)
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data:") {
				continue
			}
			var event chatwire.Object
			if err := json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(line, "data:"))), &event); err != nil {
				select {
				case failures <- err:
				default:
				}
				return
			}
			properties := chatwire.Map(event, "properties")
			if chatwire.String(event, "type") == "session.error" && chatwire.String(properties, "sessionID") == c.id {
				data, _ := json.Marshal(properties["error"])
				select {
				case failures <- fmt.Errorf("opencode session.error: %s", data):
				default:
				}
				return
			}
		}
		if ctx.Err() == nil {
			err := scanner.Err()
			if err == nil {
				err = io.EOF
			}
			select {
			case failures <- fmt.Errorf("opencode event stream: %w", err):
			default:
			}
		}
	}()
	return failures, nil
}

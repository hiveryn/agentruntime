package opencode

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/hiveryn/agentruntime"
)

// Message delivery for OpenCode: POST /session/:id/prompt_async on the TUI's
// embedded session server with a caller-minted messageID in OpenCode's
// ascending format. The server is reached through the MessageDelivery launch
// values (--port and OPENCODE_SERVER_PASSWORD; basic auth user "opencode").
// Re-sending an existing messageID upserts the message and appends a duplicate
// text part, so the id is looked up first and never re-sent once present. An
// id older than later messages is stored but not run (the run loop compares
// ids lexicographically), so a retry whose earlier id never landed mints a
// fresh one. HTTP 204 is acceptance, not processing: one 204 was followed by a
// provider error in the probes; ConfirmMessage reads the database instead.

const (
	serverUser     = "opencode"
	requestTimeout = 15 * time.Second
)

func (a *Adapter) MessageCapability() agentruntime.MessageCapability {
	return agentruntime.MessageCapability{
		Mechanism:  "opencode session server prompt_async",
		Mode:       agentruntime.MessageDeliveryPush,
		GateOnIdle: true,
		Coverage: "Pushed to the TUI's embedded server over loopback; push only while idle, or the message merges into the running loop and displaces the task's reply. " +
			"Requires the launch's --port and server password; a session started without them cannot be reached. " +
			"Receipt is the message row in OpenCode's database and an assistant message whose parentID is the message id. " +
			"PrepareMessage chooses the identity before the caller persists its attempt; DeliverMessage never replaces it. " +
			"A 204 is acceptance only. A delayed POST can still land after an absent lookup; serialize attempts and bound ambiguous retries. Verified on OpenCode 1.17.18.",
	}
}

// PrepareMessage checks the previous identity before choosing a fresh ascending
// id. It never posts; the caller can durably save the identity before sending.
func (a *Adapter) PrepareMessage(ctx context.Context, req agentruntime.DeliverRequest) (string, error) {
	if req.NativeSessionID == "" || strings.TrimSpace(req.Text) == "" {
		return "", fmt.Errorf("opencode: deliver message: session id and text are required")
	}
	if req.ServerPort <= 0 || req.ServerPassword == "" {
		return "", fmt.Errorf("opencode: deliver message: the session was not launched with a message-delivery server port and password")
	}
	client := &http.Client{Timeout: requestTimeout}
	base := "http://127.0.0.1:" + strconv.Itoa(req.ServerPort)
	if req.ProviderRef != "" {
		status, err := a.messageStatus(ctx, client, base, req.ServerPassword, req.NativeSessionID, req.ProviderRef)
		if err != nil {
			return "", err
		}
		if status == http.StatusOK {
			return req.ProviderRef, nil
		}
	}
	return newAscendingMessageID(time.Now())
}

// DeliverMessage uses the prepared identity unchanged. Lookup prevents duplicate
// text parts when a previous POST succeeded but its response was lost.
func (a *Adapter) DeliverMessage(ctx context.Context, req agentruntime.DeliverRequest) (agentruntime.DeliverResult, error) {
	if req.NativeSessionID == "" || strings.TrimSpace(req.Text) == "" || req.ServerPort <= 0 || req.ServerPassword == "" {
		return agentruntime.DeliverResult{}, agentruntime.NotDelivered(fmt.Errorf("opencode: deliver message: session, text, server port and password are required"))
	}
	if req.ProviderRef == "" {
		return agentruntime.DeliverResult{}, agentruntime.NotDelivered(fmt.Errorf("opencode: prepare and persist a provider reference before delivery"))
	}
	client := &http.Client{Timeout: requestTimeout}
	base := "http://127.0.0.1:" + strconv.Itoa(req.ServerPort)
	// Everything before prompt_async is sent establishes non-delivery.
	status, err := a.messageStatus(ctx, client, base, req.ServerPassword, req.NativeSessionID, req.ProviderRef)
	if err != nil {
		return agentruntime.DeliverResult{}, agentruntime.NotDelivered(err)
	}
	if status == http.StatusOK {
		return agentruntime.DeliverResult{Outcome: agentruntime.DeliverAlreadyPresent, ProviderRef: req.ProviderRef}, nil
	}
	messageID := req.ProviderRef
	payload := map[string]any{
		"messageID": messageID,
		"parts":     []map[string]any{{"type": "text", "text": req.Text}},
	}
	// prompt_async without an agent runs OpenCode's default agent, not the
	// session's (an architect's named agent, or whatever the TUI user last
	// selected). Reuse the agent and model of the latest user turn so the
	// message runs exactly as the TUI's own next input would.
	agent, model, err := a.latestUserSelection(ctx, client, base, req.ServerPassword, req.NativeSessionID)
	if err != nil {
		return agentruntime.DeliverResult{}, agentruntime.NotDelivered(err)
	}
	if agent != "" {
		payload["agent"] = agent
	}
	if model != nil {
		payload["model"] = model
	}
	body, _ := json.Marshal(payload)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/session/"+req.NativeSessionID+"/prompt_async", strings.NewReader(string(body)))
	if err != nil {
		return agentruntime.DeliverResult{}, agentruntime.NotDelivered(err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.SetBasicAuth(serverUser, req.ServerPassword)
	resp, err := client.Do(httpReq)
	if err != nil {
		return agentruntime.DeliverResult{}, fmt.Errorf("opencode: prompt_async: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		err := fmt.Errorf("opencode: prompt_async: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(detail)))
		// A 4xx is the server refusing the request; a 5xx may have failed
		// after storing the message, so it stays unknown.
		if resp.StatusCode < 500 {
			err = agentruntime.NotDelivered(err)
		}
		return agentruntime.DeliverResult{}, err
	}
	return agentruntime.DeliverResult{Outcome: agentruntime.DeliverAccepted, ProviderRef: messageID}, nil
}

// ProbeIdle asks the TUI's session server for the session's run state
// (GET /session/status: sessions absent from the map are idle).
func (a *Adapter) ProbeIdle(ctx context.Context, req agentruntime.DeliverRequest) (bool, error) {
	if req.NativeSessionID == "" || req.ServerPort <= 0 || req.ServerPassword == "" {
		return false, fmt.Errorf("opencode: probe idle: session, server port and password are required")
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:"+strconv.Itoa(req.ServerPort)+"/session/status", nil)
	if err != nil {
		return false, err
	}
	httpReq.SetBasicAuth(serverUser, req.ServerPassword)
	resp, err := (&http.Client{Timeout: requestTimeout}).Do(httpReq)
	if err != nil {
		return false, fmt.Errorf("opencode: probe idle: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("opencode: probe idle: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var statuses map[string]struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(body, &statuses); err != nil {
		return false, fmt.Errorf("opencode: probe idle: decode: %w", err)
	}
	status, ok := statuses[req.NativeSessionID]
	return !ok || status.Type == "idle", nil
}

// latestUserSelection returns the agent and model ({providerID, modelID}) of
// the session's most recent user message, or empty values for a session with
// none.
func (a *Adapter) latestUserSelection(ctx context.Context, client *http.Client, base, password, sessionID string) (string, map[string]any, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/session/"+sessionID+"/message", nil)
	if err != nil {
		return "", nil, err
	}
	httpReq.SetBasicAuth(serverUser, password)
	resp, err := client.Do(httpReq)
	if err != nil {
		return "", nil, fmt.Errorf("opencode: read session agent: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return "", nil, fmt.Errorf("opencode: read session agent: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", nil, fmt.Errorf("opencode: read session agent: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}
	var messages []struct {
		Info struct {
			Role  string         `json:"role"`
			Agent string         `json:"agent"`
			Model map[string]any `json:"model"`
		} `json:"info"`
	}
	if err := json.Unmarshal(data, &messages); err != nil {
		return "", nil, fmt.Errorf("opencode: read session agent: decode: %w", err)
	}
	for i := len(messages) - 1; i >= 0; i-- {
		if info := messages[i].Info; info.Role == "user" {
			return info.Agent, info.Model, nil
		}
	}
	return "", nil, nil
}

func (a *Adapter) messageStatus(ctx context.Context, client *http.Client, base, password, sessionID, messageID string) (int, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/session/"+sessionID+"/message/"+messageID, nil)
	if err != nil {
		return 0, err
	}
	httpReq.SetBasicAuth(serverUser, password)
	resp, err := client.Do(httpReq)
	if err != nil {
		return 0, fmt.Errorf("opencode: look up message %s: %w", messageID, err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)
	switch resp.StatusCode {
	case http.StatusOK, http.StatusNotFound:
		return resp.StatusCode, nil
	case http.StatusUnauthorized:
		return 0, fmt.Errorf("opencode: look up message %s: server rejected the password", messageID)
	default:
		return 0, fmt.Errorf("opencode: look up message %s: HTTP %d", messageID, resp.StatusCode)
	}
}

// ConfirmMessage reads the session's messages from OpenCode's database: the
// message row is the appearance; a completed assistant message whose parentID
// is the message id (or a later completed assistant message) the
// acknowledgement. OpenCode inserts the assistant row when it starts replying,
// so only time.completed means a reply was produced.
func (a *Adapter) ConfirmMessage(_ context.Context, req agentruntime.ConfirmRequest) (agentruntime.MessageReceipt, error) {
	if req.ProviderRef == "" {
		return agentruntime.MessageReceipt{}, nil
	}
	dbPath, sessionID, ok := strings.Cut(req.TranscriptHandle, pathSep)
	if !ok || sessionID == "" {
		return agentruntime.MessageReceipt{}, fmt.Errorf("opencode: confirm message: malformed transcript handle %q (want <dbPath>::<sessionID>)", req.TranscriptHandle)
	}
	db, err := openReadOnly(dbPath)
	if err != nil {
		return agentruntime.MessageReceipt{}, fmt.Errorf("opencode: confirm message: open db: %w", err)
	}
	defer func() { _ = db.Close() }()
	rows, err := db.Query(`SELECT id, time_created, data FROM message WHERE session_id = ? ORDER BY time_created ASC, id ASC`, sessionID)
	if err != nil {
		return agentruntime.MessageReceipt{}, fmt.Errorf("opencode: confirm message: query messages: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var receipt agentruntime.MessageReceipt
	for rows.Next() {
		var id string
		var created int64
		var raw []byte
		if err := rows.Scan(&id, &created, &raw); err != nil {
			return agentruntime.MessageReceipt{}, fmt.Errorf("opencode: confirm message: scan row: %w", err)
		}
		if at := time.UnixMilli(created).UTC(); created > 0 && at.After(receipt.RecordedThrough) {
			receipt.RecordedThrough = at
		}
		if id == req.ProviderRef {
			receipt.Appeared = true
			continue
		}
		var msg struct {
			Role     string `json:"role"`
			ParentID string `json:"parentID"`
			Time     struct {
				Completed int64 `json:"completed"`
			} `json:"time"`
		}
		if err := json.Unmarshal(raw, &msg); err != nil {
			continue
		}
		if msg.Role == "assistant" && msg.Time.Completed > 0 && (msg.ParentID == req.ProviderRef || (receipt.Appeared && msg.ParentID == "")) {
			receipt.Appeared = true
			receipt.Acknowledged = true
		}
	}
	if err := rows.Err(); err != nil {
		return agentruntime.MessageReceipt{}, fmt.Errorf("opencode: confirm message: iterate rows: %w", err)
	}
	return receipt, nil
}

// newAscendingMessageID mints an id in OpenCode's format: "msg_" + 12 hex
// digits of (unix ms * 4096 + counter) + 14 base62 characters.
func newAscendingMessageID(now time.Time) (string, error) {
	stamp := uint64(now.UnixMilli())*4096 + 1
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], stamp)
	random := make([]byte, 14)
	if _, err := rand.Read(random); err != nil {
		return "", fmt.Errorf("opencode: mint message id: %w", err)
	}
	const alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	suffix := make([]byte, 14)
	for i, b := range random {
		suffix[i] = alphabet[int(b)%len(alphabet)]
	}
	return "msg_" + hex.EncodeToString(buf[2:]) + string(suffix), nil
}

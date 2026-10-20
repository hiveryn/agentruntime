package claude

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/hiveryn/agentruntime"
)

// Message delivery for Claude Code: documented asyncRewake command hooks.
// Each SessionStart and Stop arms a waiter (the node script below) that
// long-polls the caller's ingest.Mailbox; on a message it writes the text to
// stderr and exits 2, which wakes the idle model with the text as a system
// reminder. MCP Channels were ruled out (blocked by Team org policy). The
// waiter exits 0 without an endpoint, when superseded by a newer waiter or
// when the mailbox says the session is unknown (404), and retries transport
// errors in-process: once the hook exits, nothing re-arms the session until
// its next hook event, so a lost connection must not end it.

// MessageCapability describes the asyncRewake route.
func (a *Adapter) MessageCapability() agentruntime.MessageCapability {
	return agentruntime.MessageCapability{
		Mechanism:  "claude asyncRewake hook waiter",
		Mode:       agentruntime.MessageDeliveryPull,
		GateOnIdle: true,
		Coverage: "Delivered only through a waiter armed by the session's latest SessionStart or Stop hook; " +
			"hand over only while idle, or the message joins the running turn. " +
			"A waiter past its hook timeout, or a session that never fired a hook since launch, has no waiter until its next Stop or resume. " +
			"Receipt is the transcript entry carrying the message id; Claude does not replay a message it enqueued but never ran before being killed, so redeliver when the entry is missing after the next idle.",
	}
}

// DeliverMessage is not a push: Claude receives messages through the armed
// waiter (ingest.Mailbox.Deliver).
func (a *Adapter) DeliverMessage(context.Context, agentruntime.DeliverRequest) (agentruntime.DeliverResult, error) {
	return agentruntime.DeliverResult{}, fmt.Errorf("claude: messages are handed to the session's armed asyncRewake waiter (ingest.Mailbox), not pushed")
}

// ConfirmMessage scans the session transcript for a user entry carrying the
// message id (the text embeds it) and for an assistant entry after it.
func (a *Adapter) ConfirmMessage(_ context.Context, req agentruntime.ConfirmRequest) (agentruntime.MessageReceipt, error) {
	if req.TranscriptHandle == "" || req.MessageID == "" {
		return agentruntime.MessageReceipt{}, fmt.Errorf("claude: confirm message: transcript handle and message id are required")
	}
	file, err := os.Open(req.TranscriptHandle)
	if err != nil {
		return agentruntime.MessageReceipt{}, fmt.Errorf("claude: open transcript: %w", err)
	}
	defer func() { _ = file.Close() }()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 1024*1024), 16*1024*1024)
	var receipt agentruntime.MessageReceipt
	needle := []byte(req.MessageID)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var rec struct {
			Type      string `json:"type"`
			Timestamp string `json:"timestamp"`
		}
		if err := json.Unmarshal(line, &rec); err != nil {
			continue
		}
		if rec.Timestamp != "" {
			if at, err := time.Parse(time.RFC3339Nano, rec.Timestamp); err == nil && at.After(receipt.RecordedThrough) {
				receipt.RecordedThrough = at
			}
		}
		switch rec.Type {
		case "user":
			if !receipt.Appeared && containsBytes(line, needle) {
				receipt.Appeared = true
			}
		case "assistant":
			if receipt.Appeared {
				receipt.Acknowledged = true
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return agentruntime.MessageReceipt{}, fmt.Errorf("claude: scan transcript: %w", err)
	}
	return receipt, nil
}

func containsBytes(line, needle []byte) bool {
	return strings.Contains(string(line), string(needle))
}

// messageHookSettings renders the per-launch --settings JSON that arms the
// waiter on SessionStart and Stop.
func messageHookSettings(timeout time.Duration) ([]byte, error) {
	seconds := int(timeout / time.Second)
	if seconds < 1 {
		seconds = 1
	}
	hook := map[string]any{
		"type":        "command",
		"command":     messageWaiterCommand(),
		"asyncRewake": true,
		"timeout":     seconds,
	}
	group := []any{map[string]any{"hooks": []any{hook}}}
	settings := map[string]any{"hooks": map[string]any{
		"SessionStart": group,
		"Stop":         group,
	}}
	data, err := json.Marshal(settings)
	if err != nil {
		return nil, fmt.Errorf("marshal message hook settings: %w", err)
	}
	return data, nil
}

// messageWaiterCommand is the self-contained node waiter. It must not contain
// single quotes: the command is wrapped in them.
func messageWaiterCommand() string {
	return fmt.Sprintf(`node -e '%s'`, messageWaiterScript)
}

const messageWaiterScript = `let d="";process.stdin.on("data",c=>d+=c);process.stdin.on("end",()=>{` +
	`const e=process.env.AGENTRUNTIME_HOOK_ENDPOINT||"";if(!e)process.exit(0);` +
	`let h={};try{h=JSON.parse(d||"{}")}catch(x){}` +
	`const b=JSON.stringify({agent:"claude",hook:h,env:{AGENTRUNTIME_SESSION_ID:process.env.AGENTRUNTIME_SESSION_ID||""}});` +
	`const u=new URL(e+"/claude/wait");const m=u.protocol==="https:"?require("https"):require("http");` +
	`const once=()=>new Promise(r=>{const q=m.request(u.href,{method:"POST",headers:{"Content-Type":"application/json"}},s=>{let o="";s.on("data",c=>o+=c);s.on("end",()=>r({s:s.statusCode,o}))});q.on("error",x=>r({err:String(x)}));q.write(b);q.end()});` +
	`(async()=>{for(;;){const r=await once();` +
	`if(r.s===204||r.s===404||r.s===400)process.exit(0);` +
	`if(r.s===200){let t=r.o;try{t=JSON.parse(r.o).text||r.o}catch(x){}process.stderr.write(String(t)+"\n");process.exit(2)}` +
	`await new Promise(z=>setTimeout(z,2000))}})()});`

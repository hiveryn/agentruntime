package codex

import (
	"fmt"
	"time"

	"github.com/hiveryn/agentruntime"
)

// HookCommand returns a HookCommand that posts native Codex hook events to the
// given endpoint. The generated command is a self-contained node script that
// reads the native hook JSON from stdin, wraps it in an envelope with
// received_at, hook, AGENTRUNTIME_SESSION_ID, and hook_cwd, then POSTs it to
// endpoint/codex. Only node is required at runtime — both Codex and Claude CLI
// are Node.js applications.
func HookCommand(endpoint string) agentruntime.HookCommand {
	timeout := 10 * time.Second
	cmd := buildHookCommand("codex", endpoint, timeout)
	return agentruntime.HookCommand{
		Command:       cmd,
		Endpoint:      endpoint,
		Timeout:       timeout,
		StatusMessage: "agentruntime codex hook",
	}
}

func buildHookCommand(agent, endpoint string, timeout time.Duration) string {
	timeoutMs := timeout.Milliseconds()
	extra := ""
	if agent == "claude" {
		extra = `agent:"claude",`
	}
	url := endpoint + "/" + agent
	return fmt.Sprintf(
		`node -e 'let d="";process.stdin.on("data",c=>d+=c);process.stdin.on("end",()=>{try{const h=JSON.parse(d||"{}");const b=JSON.stringify({%sreceived_at:new Date().toISOString(),hook:h,env:{AGENTRUNTIME_SESSION_ID:process.env.AGENTRUNTIME_SESSION_ID||""},hook_cwd:process.cwd()});const u=new URL(%q);const m=u.protocol==="https:"?require("https"):require("http");const r=m.request(u.href,{method:"POST",headers:{"Content-Type":"application/json"},timeout:%d},res=>res.resume());r.on("error",()=>{});r.write(b);r.end()}catch(e){}})'`,
		extra, url, timeoutMs,
	)
}

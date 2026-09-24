package claude

import (
	"fmt"
	"time"

	"github.com/hiveryn/agentruntime"
)

// HookCommand returns the endpoint-independent HookCommand that posts native
// Claude hook events. The generated command is a self-contained node script
// that reads the native hook JSON from stdin, wraps it in an envelope with
// agent, received_at, hook, AGENTRUNTIME_SESSION_ID, and hook_cwd, then POSTs
// it to $AGENTRUNTIME_HOOK_ENDPOINT/claude. When that env var is empty (a
// session not launched with StartRequest.HookEndpoint) it posts nothing. Only
// node is required at runtime — both Codex and Claude CLI are Node.js
// applications.
func HookCommand() agentruntime.HookCommand {
	timeout := 10 * time.Second
	return agentruntime.HookCommand{
		Command:       buildHookCommand(timeout),
		Timeout:       timeout,
		StatusMessage: "agentruntime claude hook",
	}
}

func buildHookCommand(timeout time.Duration) string {
	return fmt.Sprintf(
		`node -e 'let d="";process.stdin.on("data",c=>d+=c);process.stdin.on("end",()=>{try{const e=process.env.%s||"";if(!e)return;const h=JSON.parse(d||"{}");const b=JSON.stringify({agent:"claude",received_at:new Date().toISOString(),hook:h,env:{AGENTRUNTIME_SESSION_ID:process.env.AGENTRUNTIME_SESSION_ID||""},hook_cwd:process.cwd()});const u=new URL(e+"/claude");const m=u.protocol==="https:"?require("https"):require("http");const r=m.request(u.href,{method:"POST",headers:{"Content-Type":"application/json"},timeout:%d},res=>res.resume());r.on("error",()=>{});r.write(b);r.end()}catch(e){}})'`,
		agentruntime.HookEndpointEnv, timeout.Milliseconds(),
	)
}

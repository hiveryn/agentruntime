#!/usr/bin/env node
// Claude asyncRewake hook: long-polls the mailbox for this session; on a message it
// writes the text to stderr and exits 2, which Claude shows to the model as a
// system reminder and which wakes an idle session. Exit 0 when superseded.
// Transport errors (mailbox restart, dropped connection) are retried inside the same
// process: once this hook exits, nothing re-arms the session until its next hook event.
// With argument "busy" (UserPromptSubmit) it only reports that a turn started.
// Uses node:http rather than fetch: fetch's 300 s headers timeout ends idle long-polls.
import { appendFileSync } from "node:fs";
import { get } from "node:http";
const logPath = process.env.PROBE_LOG_HOOK || "/dev/null";
const log = (event, data = {}) => appendFileSync(logPath, JSON.stringify({ at: new Date().toISOString(), pid: process.pid, event, ...data }) + "\n");
let input = "";
for await (const c of process.stdin) input += c;
const h = JSON.parse(input || "{}");
log("start", { hookEvent: h.hook_event_name, session: h.session_id, source: h.source });
const base = process.env.MAILBOX_URL || "http://127.0.0.1:8795";
const call = (path) =>
  new Promise((resolve, reject) => {
    get(base + path, (res) => {
      let body = "";
      res.on("data", (c) => (body += c));
      res.on("end", () => resolve({ status: res.statusCode, body }));
      res.on("error", reject);
    }).on("error", reject);
  });

if (process.argv[2] === "busy") {
  await call(`/busy?session=${h.session_id}`).catch(() => {});
  process.exit(0);
}
for (;;) {
  try {
    const r = await call(`/wait?session=${h.session_id}&event=${h.hook_event_name}`);
    if (r.status === 204) {
      log("exit0", { status: r.status });
      process.exit(0);
    }
    if (r.status === 200) {
      const m = JSON.parse(r.body);
      log("deliver", { id: m.id });
      process.stderr.write(`Hiveryn message ${m.id}: ${m.content}\n`);
      process.exit(2);
    }
    log("retry", { status: r.status });
  } catch (e) {
    log("retry", { error: String(e) });
  }
  await new Promise((r) => setTimeout(r, 2000));
}

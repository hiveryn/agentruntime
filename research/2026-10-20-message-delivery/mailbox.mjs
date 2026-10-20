#!/usr/bin/env node
// Stand-in for the daemon side of Claude asyncRewake delivery. One long-poll waiter per
// session: a new waiter supersedes the previous one (answered 204 so its hook exits 0).
//   POST /send {"session":"S","id":"M1","content":"..."}   queue a message
//   GET  /wait?session=S&event=E                             hook long-poll
//   GET  /state                                              queue, waiters, hand-offs
//   GET  /busy?session=S                                     UserPromptSubmit: a turn started
// HOLD_WHILE_BUSY=1 releases a message only while the session is idle: after SessionStart or
// Stop and before the next UserPromptSubmit (or the turn a hand-off itself starts).
import { appendFileSync } from "node:fs";
import { createServer } from "node:http";
const logPath = process.env.PROBE_LOG || "/dev/stderr";
const log = (event, data = {}) => appendFileSync(logPath, JSON.stringify({ at: new Date().toISOString(), event, ...data }) + "\n");
const queues = new Map(), waiters = new Map(), handed = [], busy = new Set();
const hold = process.env.HOLD_WHILE_BUSY === "1";
let seq = 0;
const q = (s) => queues.get(s) || (queues.set(s, []), queues.get(s));
function pump(session) {
  const w = waiters.get(session), list = q(session);
  if (!w || list.length === 0) return;
  if (hold && busy.has(session)) return log("held", { session, id: list[0].id });
  const msg = list.shift();
  waiters.delete(session);
  busy.add(session); // the woken turn is running until its Stop
  handed.push({ ...msg, waiter: w.id, at: new Date().toISOString() });
  log("handed", { session, id: msg.id, waiter: w.id, hookEvent: w.event });
  w.res.end(JSON.stringify(msg));
}
createServer((req, res) => {
  const url = new URL(req.url, "http://x");
  let body = "";
  req.on("data", (c) => (body += c));
  req.on("end", () => {
    if (url.pathname === "/send") {
      const m = JSON.parse(body);
      q(m.session).push(m);
      log("queued", { session: m.session, id: m.id });
      pump(m.session);
      return res.end(JSON.stringify({ queued: true }));
    }
    if (url.pathname === "/busy") {
      const session = url.searchParams.get("session");
      busy.add(session);
      log("busy", { session });
      return res.end();
    }
    if (url.pathname === "/wait") {
      const session = url.searchParams.get("session"), event = url.searchParams.get("event");
      if (event === "Stop" || event === "SessionStart") busy.delete(session);
      const prev = waiters.get(session);
      if (prev) { log("superseded", { session, waiter: prev.id }); prev.res.statusCode = 204; prev.res.end(); }
      const w = { id: ++seq, event, res };
      waiters.set(session, w);
      log("waiting", { session, waiter: w.id, hookEvent: event });
      res.on("close", () => { if (waiters.get(session) === w && !res.writableEnded) { waiters.delete(session); log("waiter_gone", { session, waiter: w.id }); } });
      return pump(session);
    }
    if (url.pathname === "/state") {
      return res.end(JSON.stringify({ queues: Object.fromEntries(queues), waiters: [...waiters].map(([s, w]) => ({ s, id: w.id, event: w.event })), handed }, null, 1));
    }
    res.statusCode = 404; res.end();
  });
}).listen(Number(process.env.MAILBOX_PORT || 8795), "127.0.0.1", () => log("listening"));

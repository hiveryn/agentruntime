#!/usr/bin/env node
// Isolated MCP stdio probe server for measuring how long each agent CLI lets a
// single tools/call stay pending. Dependency-free; newline-delimited JSON-RPC.
//
// Tool wait_probe({seconds, progress_every}) holds the call open for `seconds`,
// optionally emitting notifications/progress every `progress_every` seconds when
// the client supplied a progressToken, then returns PROBE_DONE text. Every
// message and lifecycle event is appended to $PROBE_LOG with a timestamp.
import { appendFileSync } from "node:fs";
import { createInterface } from "node:readline";

const logPath = process.env.PROBE_LOG || "/dev/stderr";
const started = Date.now();
const log = (event, data = {}) =>
  appendFileSync(
    logPath,
    JSON.stringify({ at: new Date().toISOString(), t: ((Date.now() - started) / 1000).toFixed(1), pid: process.pid, event, ...data }) + "\n",
  );

const send = (msg) => process.stdout.write(JSON.stringify(msg) + "\n");
const pending = new Map();

const tools = [
  {
    name: "wait_probe",
    description:
      "Diagnostic probe. Blocks for the requested number of seconds and then returns a line of text. Call it exactly once with the arguments you were given.",
    inputSchema: {
      type: "object",
      properties: {
        seconds: { type: "number", description: "How long to block before returning." },
        progress_every: { type: "number", description: "Seconds between progress notifications; 0 disables." },
      },
      required: ["seconds"],
    },
  },
];

function handleCall(msg) {
  const args = msg.params?.arguments || {};
  const seconds = Number(args.seconds) || 0;
  const every = Number(args.progress_every) || 0;
  const token = msg.params?._meta?.progressToken;
  const callStart = Date.now();
  log("call_start", { id: msg.id, seconds, every, progressToken: token ?? null });
  const timers = [];
  if (every > 0 && token !== undefined) {
    let n = 0;
    timers.push(
      setInterval(() => {
        n += 1;
        send({ jsonrpc: "2.0", method: "notifications/progress", params: { progressToken: token, progress: n * every, total: seconds, message: `waited ${n * every}s` } });
        log("progress_sent", { id: msg.id, progress: n * every });
      }, every * 1000),
    );
  }
  timers.push(
    setTimeout(() => {
      timers.forEach(clearInterval);
      pending.delete(msg.id);
      const elapsed = ((Date.now() - callStart) / 1000).toFixed(1);
      send({ jsonrpc: "2.0", id: msg.id, result: { content: [{ type: "text", text: `PROBE_DONE after ${elapsed}s` }] } });
      log("call_done", { id: msg.id, elapsed });
    }, seconds * 1000),
  );
  pending.set(msg.id, { timers, callStart });
}

createInterface({ input: process.stdin }).on("line", (line) => {
  if (!line.trim()) return;
  let msg;
  try {
    msg = JSON.parse(line);
  } catch {
    log("bad_json", { line });
    return;
  }
  log("recv", { method: msg.method ?? null, id: msg.id ?? null, params: msg.method === "tools/call" || msg.method === "notifications/cancelled" ? msg.params : undefined });
  switch (msg.method) {
    case "initialize":
      send({
        jsonrpc: "2.0",
        id: msg.id,
        result: {
          protocolVersion: msg.params?.protocolVersion || "2025-06-18",
          capabilities: { tools: {} },
          serverInfo: { name: "wait-probe", version: "1.0.0" },
        },
      });
      break;
    case "tools/list":
      send({ jsonrpc: "2.0", id: msg.id, result: { tools } });
      break;
    case "tools/call":
      if (msg.params?.name === "wait_probe") handleCall(msg);
      else send({ jsonrpc: "2.0", id: msg.id, error: { code: -32602, message: "unknown tool" } });
      break;
    case "notifications/cancelled": {
      const id = msg.params?.requestId;
      const p = pending.get(id);
      if (p) {
        p.timers.forEach(clearInterval);
        pending.delete(id);
        log("call_cancelled", { id, elapsed: ((Date.now() - p.callStart) / 1000).toFixed(1), reason: msg.params?.reason });
      }
      break;
    }
    case "ping":
      send({ jsonrpc: "2.0", id: msg.id, result: {} });
      break;
    default:
      if (msg.id !== undefined && msg.method) send({ jsonrpc: "2.0", id: msg.id, error: { code: -32601, message: "method not found" } });
  }
});

process.stdin.on("end", () => {
  log("stdin_end", { pending: [...pending.keys()] });
  process.exit(0);
});
process.on("SIGTERM", () => {
  log("sigterm", { pending: [...pending.keys()] });
  process.exit(0);
});
log("server_start", { argv: process.argv.slice(2) });

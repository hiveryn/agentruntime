#!/usr/bin/env node
// Claude Code channel probe: a dependency-free stdio MCP server that declares the
// experimental `claude/channel` capability and forwards HTTP POSTs on
// 127.0.0.1:$CHANNEL_PORT into the session as `notifications/claude/channel`.
//
//   POST /send  {"id":"MSG-1","content":"..."}  -> one channel event, meta message_id=id
//   GET  /state                                 -> what was sent and acknowledged
//
// It also exposes `ack_message(message_id)` so the agent can confirm handling,
// which separates transport acceptance (the write to stdout), appearance in the
// conversation (transcript) and agent acknowledgement (this tool call).
// Every message and event is appended to $PROBE_LOG.
import { appendFileSync } from "node:fs";
import { createServer } from "node:http";
import { createInterface } from "node:readline";

const logPath = process.env.PROBE_LOG || "/dev/stderr";
const port = Number(process.env.CHANNEL_PORT || 8790);
const log = (event, data = {}) =>
  appendFileSync(logPath, JSON.stringify({ at: new Date().toISOString(), pid: process.pid, event, ...data }) + "\n");
const send = (msg) => process.stdout.write(JSON.stringify(msg) + "\n");

const sent = [];
const acks = [];
let initialized = false;

const tools = [
  {
    name: "ack_message",
    description: "Acknowledge a Hiveryn notification you received through the hiveryn channel.",
    inputSchema: {
      type: "object",
      properties: { message_id: { type: "string", description: "The message_id attribute of the <channel> tag." } },
      required: ["message_id"],
    },
  },
];

createInterface({ input: process.stdin }).on("line", (line) => {
  let msg;
  try {
    msg = JSON.parse(line);
  } catch {
    return log("bad_json", { line });
  }
  log("recv", { msg });
  if (msg.method === "initialize") {
    send({
      jsonrpc: "2.0",
      id: msg.id,
      result: {
        // Older revision on purpose: Claude does not register channels on 2026-07-28 connections.
        protocolVersion: "2025-06-18",
        capabilities: { tools: {}, experimental: { "claude/channel": {} } },
        serverInfo: { name: "hiveryn-channel-probe", version: "0.0.1" },
        instructions:
          'Hiveryn notifications arrive as <channel source="hiveryn" message_id="...">. Treat each as a message from Hiveryn and call ack_message with its message_id once you have read it.',
      },
    });
  } else if (msg.method === "notifications/initialized") {
    initialized = true;
  } else if (msg.method === "tools/list") {
    send({ jsonrpc: "2.0", id: msg.id, result: { tools } });
  } else if (msg.method === "tools/call") {
    const id = msg.params?.arguments?.message_id;
    acks.push({ id, at: new Date().toISOString() });
    log("ack", { message_id: id });
    send({ jsonrpc: "2.0", id: msg.id, result: { content: [{ type: "text", text: `acknowledged ${id}` }] } });
  } else if (msg.id !== undefined) {
    send({ jsonrpc: "2.0", id: msg.id, error: { code: -32601, message: "method not found" } });
  }
});
process.stdin.on("end", () => {
  log("stdin_end");
  process.exit(0);
});

createServer((req, res) => {
  let body = "";
  req.on("data", (c) => (body += c));
  req.on("end", () => {
    if (req.method === "GET" && req.url === "/state") {
      res.end(JSON.stringify({ initialized, sent, acks }));
      return;
    }
    if (req.method === "POST" && req.url === "/send") {
      const { id, content } = JSON.parse(body || "{}");
      const note = { method: "notifications/claude/channel", params: { content, meta: { message_id: id } } };
      const ok = process.stdout.write(JSON.stringify({ jsonrpc: "2.0", ...note }) + "\n");
      sent.push({ id, at: new Date().toISOString(), initialized });
      log("sent", { id, initialized, writeReturned: ok });
      res.end(JSON.stringify({ accepted: true, initialized }));
      return;
    }
    res.statusCode = 404;
    res.end();
  });
}).listen(port, "127.0.0.1", () => log("listening", { port }));

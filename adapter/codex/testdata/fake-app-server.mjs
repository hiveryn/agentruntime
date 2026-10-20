#!/usr/bin/env node
// Fake `codex app-server` for tests: speaks the JSON-RPC subset the adapter
// uses over stdio and records the queue in $FAKE_QUEUE_FILE (JSON array).
import { readFileSync, writeFileSync, existsSync } from "node:fs";
import { createInterface } from "node:readline";
const file = process.env.FAKE_QUEUE_FILE;
const load = () => (existsSync(file) ? JSON.parse(readFileSync(file, "utf8")) : []);
const save = (q) => writeFileSync(file, JSON.stringify(q));
const send = (o) => process.stdout.write(JSON.stringify(o) + "\n");
let initialized = false;
createInterface({ input: process.stdin }).on("line", (line) => {
  const msg = JSON.parse(line);
  if (msg.method === "initialized") return;
  if (msg.method === "initialize") {
    initialized = true;
    send({ jsonrpc: "2.0", method: "serverNotice", params: { note: "noise before the answer" } });
    return send({ jsonrpc: "2.0", id: msg.id, result: { userAgent: "fake" } });
  }
  if (!initialized) return send({ jsonrpc: "2.0", id: msg.id, error: { code: -32002, message: "not initialized" } });
  if (process.env.CODEX_HOME !== process.env.FAKE_EXPECT_HOME) {
    return send({ jsonrpc: "2.0", id: msg.id, error: { code: 1, message: "wrong CODEX_HOME " + process.env.CODEX_HOME } });
  }
  if (msg.method === "thread/queue/list") {
    const q = load().filter((i) => i.threadId === msg.params.threadId);
    // Two pages, to exercise the cursor.
    if (!msg.params.cursor && q.length > 1) return send({ jsonrpc: "2.0", id: msg.id, result: { data: q.slice(0, 1), nextCursor: "c1" } });
    if (msg.params.cursor === "c1") return send({ jsonrpc: "2.0", id: msg.id, result: { data: q.slice(1), nextCursor: null } });
    return send({ jsonrpc: "2.0", id: msg.id, result: { data: q, nextCursor: null } });
  }
  if (msg.method === "thread/queue/add") {
    const q = load();
    const item = { id: "qs-" + (q.length + 1), threadId: msg.params.threadId, clientUserMessageId: msg.params.clientUserMessageId, text: msg.params.input[0].text };
    q.push(item);
    save(q);
    if (process.env.FAKE_LOSE_ADD_RESPONSE === "1") process.exit(0);
    return send({ jsonrpc: "2.0", id: msg.id, result: { queuedSubmission: item } });
  }
  send({ jsonrpc: "2.0", id: msg.id, error: { code: -32601, message: "unknown " + msg.method } });
});

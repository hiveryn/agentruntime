#!/usr/bin/env node
// Idempotent Codex enqueue prototype over the app-server JSON-RPC protocol (stdio).
//
//   codex-queue.mjs add  <threadId> <clientMessageId> <text>   enqueue unless already queued
//   codex-queue.mjs list <threadId>                             print queued submissions
//   codex-queue.mjs del  <threadId> <queuedSubmissionId>        delete one queued submission
//
// Unlike `codex queue`, which mints a fresh client message id per call, this sets
// clientUserMessageId to the caller's stable message id and checks thread/queue/list first,
// so a retry after an ambiguous failure does not enqueue a second copy while the first is
// still queued. After dispatch the item leaves the queue; the rollout's UserMessage item keeps
// the id as `client_id`, which is the caller's receipt evidence.
// thread/queue/* are experimental app-server methods (initialize with experimentalApi: true).
// Runs an embedded `codex app-server` over stdio, which writes the same durable queue
// ($CODEX_HOME/queue_1.sqlite) that a running TUI or shared daemon polls. A production client
// should prefer the shared daemon when it runs: its control socket
// ($CODEX_HOME/app-server-control/app-server-control.sock) speaks JSON-RPC over WebSocket, so
// `codex app-server proxy` only relays bytes for a WebSocket-speaking client.
import { spawn } from "node:child_process";
import { existsSync } from "node:fs";
import { join } from "node:path";
import { createInterface } from "node:readline";

const [cmd, threadId, a, b] = process.argv.slice(2);
const home = process.env.CODEX_HOME || join(process.env.HOME, ".codex");
const daemonRunning = existsSync(join(home, "app-server-control", "app-server-control.sock"));
const child = spawn("codex", ["app-server"], { stdio: ["pipe", "pipe", "inherit"] });
const pending = new Map();
let next = 1;
createInterface({ input: child.stdout }).on("line", (line) => {
  const msg = JSON.parse(line);
  if (msg.id !== undefined && pending.has(msg.id)) {
    const { resolve, reject } = pending.get(msg.id);
    pending.delete(msg.id);
    msg.error ? reject(new Error(`${msg.error.code}: ${msg.error.message}`)) : resolve(msg.result);
  }
});
const request = (method, params) =>
  new Promise((resolve, reject) => {
    const id = next++;
    pending.set(id, { resolve, reject });
    child.stdin.write(JSON.stringify({ jsonrpc: "2.0", id, method, params }) + "\n");
  });

async function listAll() {
  const items = [];
  let cursor;
  do {
    const page = await request("thread/queue/list", { threadId, cursor });
    items.push(...page.data);
    cursor = page.nextCursor;
  } while (cursor);
  return items;
}

try {
  await request("initialize", { clientInfo: { name: "hiveryn-probe", version: "0" }, capabilities: { experimentalApi: true } });
  child.stdin.write(JSON.stringify({ jsonrpc: "2.0", method: "initialized" }) + "\n");
  let out;
  if (cmd === "add") {
    const existing = (await listAll()).find((s) => s.clientUserMessageId === a);
    out = existing
      ? { outcome: "already_queued", queued: existing }
      : { outcome: "queued", queued: (await request("thread/queue/add", { threadId, clientUserMessageId: a, input: [{ type: "text", text: b, text_elements: [] }] })).queuedSubmission };
  } else if (cmd === "list") {
    out = await listAll();
  } else if (cmd === "del") {
    out = await request("thread/queue/delete", { threadId, queuedSubmissionId: a });
  } else {
    throw new Error("usage: add|list|del");
  }
  console.log(JSON.stringify({ daemonRunning, ...((Array.isArray(out) && { items: out }) || out) }));
} catch (e) {
  console.error(String(e));
  process.exitCode = 1;
} finally {
  child.stdin.end();
  child.kill();
}

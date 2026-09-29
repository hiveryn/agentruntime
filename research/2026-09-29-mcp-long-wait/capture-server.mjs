#!/usr/bin/env node
// Local fake model endpoint: records the tool names each CLI offers the model
// (flattening Codex "namespace"/additional_tools entries) and answers 400 so the
// CLI stops. Usage: node capture-server.mjs <port> <out.jsonl>; CAPTURE_RAW=<file>
// additionally stores headers and (truncated) bodies.
import { createServer } from "node:http";
import { appendFileSync } from "node:fs";
const [port, out] = [Number(process.argv[2]), process.argv[3]];
const flat = (ts, p = "") =>
  ts.flatMap((t) => (t.type === "namespace" && t.tools ? flat(t.tools, t.name + ".") : [p + (t.name || t.function?.name || t.type)]));
createServer((req, res) => {
  let body = "";
  req.on("data", (c) => (body += c));
  req.on("end", () => {
    let tools = null;
    try {
      const j = JSON.parse(body);
      const extra = (Array.isArray(j.input) ? j.input : []).filter((i) => i.type === "additional_tools").flatMap((i) => i.tools || []);
      tools = flat([...(j.tools || []), ...extra]);
    } catch {}
    if (process.env.CAPTURE_RAW) appendFileSync(process.env.CAPTURE_RAW, JSON.stringify({ headers: req.headers, body: body.slice(0, 200000) }) + "\n");
    appendFileSync(out, JSON.stringify({ at: new Date().toISOString(), method: req.method, url: req.url, tools }) + "\n");
    res.writeHead(400, { "content-type": "application/json" });
    res.end(JSON.stringify({ type: "error", error: { type: "invalid_request_error", message: "capture-server: request recorded" } }));
  });
}).listen(port, "127.0.0.1");

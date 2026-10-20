#!/usr/bin/env node
// Send a message to an OpenCode session through its server: oc-send.mjs <port> <sessionID> <text> [messageID]
// Without messageID it mints one in OpenCode's ascending format (msg_ + 12 hex time/counter +
// 14 base62), which a caller must persist to retry under the same ID.
import { randomBytes } from "node:crypto";
const [port, session, text, given] = process.argv.slice(2);
function ascending() {
  const now = BigInt(Date.now()) * 0x1000n + 1n;
  const b = Buffer.alloc(6);
  for (let i = 0; i < 6; i++) b[i] = Number((now >> BigInt(40 - 8 * i)) & 0xffn);
  const chars = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz";
  return "msg_" + b.toString("hex") + [...randomBytes(14)].map((x) => chars[x % 62]).join("");
}
const messageID = given || ascending();
const t0 = Date.now();
const r = await fetch(`http://127.0.0.1:${port}/session/${session}/prompt_async`, {
  method: "POST",
  headers: {
    "content-type": "application/json",
    ...(process.env.OPENCODE_SERVER_PASSWORD && { authorization: "Basic " + Buffer.from("opencode:" + process.env.OPENCODE_SERVER_PASSWORD).toString("base64") }),
  },
  body: JSON.stringify({
    messageID,
    parts: [{ type: "text", text }],
    // OC_MODEL=provider/model overrides the session model for this message.
    ...(process.env.OC_MODEL && { model: { providerID: process.env.OC_MODEL.split("/")[0], modelID: process.env.OC_MODEL.split("/").slice(1).join("/") } }),
  }),
});
console.log(JSON.stringify({ messageID, status: r.status, body: await r.text(), ms: Date.now() - t0, at: new Date().toISOString() }));

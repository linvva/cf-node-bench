import assert from "node:assert/strict";
import test from "node:test";
import worker from "./worker.js";

const validBody = {
  botToken: "123456:abcdefghijklmnopqrstuvwxyz",
  method: "sendMessage",
  payload: { chat_id: "123", text: "测速完成" },
};

test("rejects requests without the relay key", async () => {
  const response = await worker.fetch(new Request("https://relay.example/telegram", {
    method: "POST",
    body: JSON.stringify(validBody),
  }), { RELAY_KEY: "secret" });
  assert.equal(response.status, 401);
});

test("rejects methods outside the Telegram allowlist", async () => {
  const response = await worker.fetch(new Request("https://relay.example/telegram", {
    method: "POST",
    headers: { Authorization: "Bearer secret" },
    body: JSON.stringify({ ...validBody, method: "getUpdates" }),
  }), { RELAY_KEY: "secret" });
  assert.equal(response.status, 400);
  assert.match((await response.json()).description, /getChat/);
});

test("forwards only to Telegram and preserves retry information", async () => {
  const originalFetch = globalThis.fetch;
  let upstreamURL = "";
  let upstreamBody;
  globalThis.fetch = async (url, init) => {
    upstreamURL = String(url);
    upstreamBody = JSON.parse(init.body);
    return new Response(JSON.stringify({ ok: false, parameters: { retry_after: 7 } }), {
      status: 429,
      headers: { "Content-Type": "application/json" },
    });
  };
  try {
    const response = await worker.fetch(new Request("https://relay.example/telegram", {
      method: "POST",
      headers: { Authorization: "Bearer secret" },
      body: JSON.stringify(validBody),
    }), { RELAY_KEY: "secret" });
    assert.equal(upstreamURL, `https://api.telegram.org/bot${validBody.botToken}/sendMessage`);
    assert.deepEqual(upstreamBody, validBody.payload);
    assert.equal(response.status, 429);
    assert.equal(response.headers.get("Retry-After"), "7");
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("rejects oversized message text", async () => {
  const response = await worker.fetch(new Request("https://relay.example/telegram", {
    method: "POST",
    headers: { Authorization: "Bearer secret" },
    body: JSON.stringify({ ...validBody, payload: { chat_id: "123", text: "x".repeat(4001) } }),
  }), { RELAY_KEY: "secret" });
  assert.equal(response.status, 400);
});

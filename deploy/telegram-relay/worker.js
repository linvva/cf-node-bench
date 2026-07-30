const MAX_BODY_BYTES = 64 * 1024;
const MAX_MESSAGE_CHARS = 4000;
const ALLOWED_METHODS = new Set(["getChat", "sendMessage"]);

function jsonResponse(body, status, extraHeaders = {}) {
  return new Response(JSON.stringify(body), {
    status,
    headers: {
      "Content-Type": "application/json; charset=utf-8",
      "Cache-Control": "no-store",
      ...extraHeaders,
    },
  });
}

async function readLimitedBody(request) {
  const declaredLength = Number(request.headers.get("Content-Length") || 0);
  if (declaredLength > MAX_BODY_BYTES) throw new Error("BODY_TOO_LARGE");
  if (!request.body) return "";

  const reader = request.body.getReader();
  const chunks = [];
  let size = 0;
  while (true) {
    const { done, value } = await reader.read();
    if (done) break;
    size += value.byteLength;
    if (size > MAX_BODY_BYTES) {
      await reader.cancel();
      throw new Error("BODY_TOO_LARGE");
    }
    chunks.push(value);
  }

  const body = new Uint8Array(size);
  let offset = 0;
  for (const chunk of chunks) {
    body.set(chunk, offset);
    offset += chunk.byteLength;
  }
  return new TextDecoder().decode(body);
}

function validateRequest(value) {
  if (!value || typeof value !== "object" || Array.isArray(value)) return "请求体必须是 JSON 对象";
  if (typeof value.botToken !== "string" || !/^\d+:[A-Za-z0-9_-]+$/.test(value.botToken) || value.botToken.length > 256) return "Bot Token 格式无效";
  if (!ALLOWED_METHODS.has(value.method)) return "仅允许 getChat 或 sendMessage";
  if (!value.payload || typeof value.payload !== "object" || Array.isArray(value.payload)) return "payload 必须是 JSON 对象";

  const allowedKeys = value.method === "getChat" ? new Set(["chat_id"]) : new Set(["chat_id", "text"]);
  if (Object.keys(value.payload).some((key) => !allowedKeys.has(key))) return "payload 包含不允许的字段";
  const chatID = value.payload.chat_id;
  if ((typeof chatID !== "string" && typeof chatID !== "number") || String(chatID).length === 0 || String(chatID).length > 128) return "Chat ID 格式无效";
  if (value.method === "sendMessage" && (typeof value.payload.text !== "string" || value.payload.text.length === 0 || value.payload.text.length > MAX_MESSAGE_CHARS)) return `消息长度必须在 1 到 ${MAX_MESSAGE_CHARS} 个字符之间`;
  return "";
}

export default {
  async fetch(request, env) {
    const url = new URL(request.url);
    if (url.pathname !== "/telegram") return jsonResponse({ ok: false, description: "Not found" }, 404);
    if (request.method !== "POST") return jsonResponse({ ok: false, description: "Method not allowed" }, 405, { Allow: "POST" });
    if (!env.RELAY_KEY) return jsonResponse({ ok: false, description: "Relay is not configured" }, 500);
    if (request.headers.get("Authorization") !== `Bearer ${env.RELAY_KEY}`) return jsonResponse({ ok: false, description: "Unauthorized" }, 401);

    let value;
    try {
      value = JSON.parse(await readLimitedBody(request));
    } catch (error) {
      const tooLarge = error instanceof Error && error.message === "BODY_TOO_LARGE";
      return jsonResponse({ ok: false, description: tooLarge ? "Request body too large" : "Invalid JSON body" }, tooLarge ? 413 : 400);
    }
    const validationError = validateRequest(value);
    if (validationError) return jsonResponse({ ok: false, description: validationError }, 400);

    let upstream;
    try {
      upstream = await fetch(`https://api.telegram.org/bot${value.botToken}/${value.method}`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(value.payload),
      });
    } catch {
      return jsonResponse({ ok: false, description: "Telegram upstream unavailable" }, 502);
    }

    let result;
    try {
      result = await upstream.json();
    } catch {
      return jsonResponse({ ok: false, description: "Telegram returned an invalid response" }, 502);
    }
    const retryAfter = upstream.headers.get("Retry-After") || result?.parameters?.retry_after;
    const headers = retryAfter ? { "Retry-After": String(retryAfter) } : {};
    return jsonResponse(result, upstream.status, headers);
  },
};

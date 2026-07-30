# Telegram 专属中继

此 Worker 只把 CF Node Bench 的 `getChat` 和 `sendMessage` 请求转发到固定的 Telegram Bot API，用于本机无法直连 Telegram 的网络环境。Bot Token、Chat ID 和消息内容仍由 CF Node Bench 保存在本机，仅在请求时通过 HTTPS 交给 Worker；Worker 只保存用于鉴权的 `RELAY_KEY`。

必须把它部署到你自己的 Cloudflare 账户并仅供自己使用。不要使用或运营公共中继：中继运行方能够看到经由它转发的 Bot Token、Chat ID 和消息内容。

## 部署

需要 Node.js 20+ 和 Cloudflare 账户：

```bash
cd deploy/telegram-relay
pnpm dlx wrangler deploy
pnpm dlx wrangler secret put RELAY_KEY
```

`RELAY_KEY` 请使用独立生成的高强度随机值，不要与 Bot Token 相同。也可以在 Cloudflare Dashboard 中创建 Worker，粘贴 `worker.js`，再在 Worker 的 Settings / Variables and Secrets 中添加加密 Secret `RELAY_KEY`。

部署完成后，在 CF Node Bench 的“发布 / Telegram Bot”中选择“专属中继”，填写：

- 中继 URL：`https://<你的 Worker 域名>/telegram`
- 中继访问密钥：刚才设置的 `RELAY_KEY`
- Bot Token、Chat ID：仍填写在应用内，不需要配置为 Worker 变量

“测试连接”会通过 Worker 调用 Telegram `getChat`，不会发送测试消息。模板不记录请求体，也不会持久化 Bot Token、Chat ID 或消息；它拒绝其他 Telegram 方法、额外 payload 字段及超限消息，因此不是通用代理。

如果 Worker、中继密钥或访问日志配置发生泄露，请立即轮换 `RELAY_KEY`；如果 Bot Token 可能被看到，还应通过 BotFather 轮换 Bot Token。不要为该 Worker 启用会记录请求正文的第三方日志或调试服务。

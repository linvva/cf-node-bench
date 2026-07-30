# CF Node Bench

CF Node Bench 是一个跨平台 Cloudflare 候选 IP 测速工具。它在应用实际运行的设备上完成数据源获取、TCP 与 HTTPS 可用性探测、真实下载测速和综合评分，帮助筛选低延迟、高带宽、高可用节点。

![CF Node Bench 测速工作台](docs/images/workbench.png)

## 下载与运行

从 [GitHub Releases](https://github.com/linvva/cf-node-bench/releases) 下载与系统匹配的压缩包，解压后直接运行：

| 系统                        | 下载文件              | 运行方式                                                                       |
| --------------------------- | --------------------- | ------------------------------------------------------------------------------ |
| Windows 10/11 x64           | `windows-amd64.zip`   | 运行 `CF-Node-Bench.exe`，系统需有 WebView2 Runtime                            |
| macOS Intel / Apple Silicon | `macos-universal.zip` | 打开 `CF-Node-Bench.app`；当前构建未签名，首次运行可在 Finder 中右键选择“打开” |
| Linux x64                   | `linux-amd64.tar.gz`  | 运行 `./CF-Node-Bench`，系统需有 GTK3 和 WebKitGTK 4.1 运行库                  |

每个 Release 同时提供 `SHA256SUMS.txt`。压缩包无需安装本应用；系统 WebView、GTK 等桌面运行库不包含在压缩包内。

## 使用方法

1. 打开“数据源”，确认至少启用了一个 HTTP 数据源。内置示例可以修改或删除，也可以添加自己的地址。数据源支持 `IPv4:port#国家代码`、`IPv4:port`、裸 IPv4、空白分隔文本，以及包含 `ip`/`host`、`port`、`country`/`cc` 的 JSON。
2. 打开“设置”，按当前设备和网络调整并发、超时、探测次数、候选池和最大下载量。端口或允许国家为空表示不限；排除国家始终优先于允许国家。
3. 回到“测速工作台”并点击“开始测速”。界面会持续显示数据源、解析/过滤、TCP、HTTPS、带宽和排序阶段的输入、通过、失败、耗时及失败原因；测速过程中可以随时取消。
4. 完成后可在结果表格中排序、筛选、选择、复制或导出。点击任意节点可查看 TCP、HTTPS、带宽和各部分得分明细。
5. 如需自动分发结果，打开“发布”配置 Cloudflare、GitHub 仓库、GitHub Gist 或 Telegram。各目标相互独立，可以同时启用。保存后，每次成功完成测速都会在后台发布；取消测速不会发布，发布失败也不会改变测速结果。结果表格会显示各目标状态并允许手动重试。

HTTPS 探测通过候选 `IP:port` 建立连接，但 TLS SNI 和 HTTP Host 均使用 `speed.cloudflare.com` 并正常验证证书。TCP 和 HTTPS 成功率是硬门槛，低可用节点不会仅凭高带宽进入最终排名。

## 发布结果

TXT、GitHub 仓库文件、Gist 文件和 Telegram 详细列表共用发布页中的输出字段，默认格式为：

```text
IP:PORT#国家|HTTP44ms|186Mbps
```

- Cloudflare `A`：默认模式，只发布端口为 443 的 IPv4，记录内容为纯 IP；`Proxied` 只在此模式生效。
- Cloudflare `TXT`：发布全部最终节点，每个节点使用上述共享格式。
- GitHub 仓库：通过 Contents API 创建或更新指定分支中的文件；内容没有变化时不会产生新提交。
- GitHub Gist：使用独立 Token 更新用户预先创建的 Gist 中的指定文件，并在结果页提供 Raw 链接。内容没有变化时不发送更新请求；目标文件不存在时会新增，Gist 中的其他文件不会被修改或删除。更改文件名不会删除旧文件。应用不负责创建 Gist 或改变其可见性；“测试连接”只验证 Token 和 Gist 可访问，不执行写操作，写权限会在实际发布时验证。
- Telegram“仅汇总”：发送测速耗时、通过节点数及 Cloudflare、GitHub 仓库、Gist 发布状态，不包含 IP。
- Telegram“汇总与节点列表”：先发相同汇总，再分段发送全部最终节点。
- Telegram 默认由应用直连 Bot API。如果本地网络无法访问 Telegram，可选择“专属中继”，点击发布页中的“部署 Worker”即可查看完整引导、内置 `worker.js` 并一键复制；仓库文件位于 [`deploy/telegram-relay/worker.js`](deploy/telegram-relay/worker.js)，更多说明见[专属中继部署文档](deploy/telegram-relay/README.md)。“测试连接”只通过中继调用 `getChat`，不会发送测试消息。

Cloudflare 类型切换时，应用会在同一批请求中删除同名且 comment 为 `Managed by CF Node Bench` 的旧 A/TXT 记录，再创建当前类型。没有该标记的用户记录不会被删除；新记录集合为空时也不会删除旧记录。批处理完成后，各 DNS 记录仍可能分别传播，并不保证传播过程原子完成。

建议为 Cloudflare Token 仅授予目标 Zone 的 DNS 编辑权限；GitHub 仓库 Token 仅授予目标仓库 Contents 写权限；Gist 使用另一枚仅授予 `Gists: write` 的 Token。Secret Gist 只是不会出现在公开列表中，并不是真正的私有存储，任何获得链接的人都能访问；不要发布敏感内容。Telegram Bot 只需能够向配置的 Chat ID 发送消息。

Telegram 中继必须部署在用户自己的账户中并仅供本人使用。Bot Token、Chat ID 和消息仍保存在本机，但中继模式下会在每次请求时经 HTTPS 传给 Worker；中继运行方能够看到这些内容，因此不要使用第三方公共中继。随项目提供的 Worker 仅保存独立的 `RELAY_KEY`，只允许 `getChat` 和 `sendMessage`，上游固定为 Telegram，并限制请求字段和消息长度。

## 本地数据与网络

设置、发布凭据、数据源和最近运行历史保存在操作系统标准配置目录下的 `CF Node Bench/data.json`。文件使用原子写入并设置为当前用户可读写的 `0600` 权限；Cloudflare、GitHub 仓库、Gist、Telegram Bot Token 和中继访问密钥均以明文保存，但不会返回给前端、写入测速历史或错误日志。发布页中凭据留空表示保留原值，必须点击对应的清除按钮才能删除。

测速会访问用户配置的数据源以及 Cloudflare 的 `speed.cloudflare.com`；启用发布后还会访问相应官方 API。所有 Go HTTP Transport 均显式禁用系统和环境代理；中继模式是应用直连用户配置的 Worker URL，不会启用系统代理。结果来自当前运行设备的真实网络环境；浏览器前端预览只使用模拟桥接，不执行真实网络探测或外部发布。

## 技术栈

- Go 1.26 与 Wails v2.13：探测、调度、评分、存储和桌面容器
- React 19、TypeScript、Vite 8：前端应用
- HeroUI v3 与 Tailwind CSS v4：基础组件、布局和主题
- Apache ECharts：历史与指标图表
- lucide-react：界面图标
- pnpm 11：前端依赖与 workspace 管理

精确依赖版本记录在 `go.sum` 和 `pnpm-lock.yaml`。

## 本地开发

需要 Go 1.26、Node.js 24、pnpm 11、Wails v2.13，以及当前平台的 [Wails 系统依赖](https://wails.io/docs/gettingstarted/installation/)。

```bash
go install github.com/wailsapp/wails/v2/cmd/wails@v2.13.0
pnpm install --frozen-lockfile
wails dev
```

只调试前端时运行：

```bash
pnpm dev
```

## 测试与构建

```bash
go test ./...
pnpm test
pnpm test:relay
pnpm typecheck
pnpm test:e2e
pnpm build
wails build
```

Go 网络测试使用 `httptest` 或本机监听器，不依赖公网。Playwright 覆盖 1280×800 浅色和 1440×900 深色布局。Ubuntu 24.04 使用 WebKitGTK 4.1 构建时需执行：

```bash
wails build -tags webkit2_41
```

## 发布流程

推送以 `v` 开头的版本 Tag 会触发 `.github/workflows/release.yml`。流水线先运行 Go 与前端检查，再并行构建 Windows x64、Linux x64 和 macOS Universal 免安装压缩包，最后创建 GitHub Release 并生成 SHA-256 校验文件。

```bash
git tag -a v0.3.0 -m "v0.3.0"
git push origin v0.3.0
```

## 项目结构

```text
internal/source   HTTP 获取、解析与标准化
internal/probe    TCP、HTTPS、带宽和统计量
internal/ranking  硬门槛、归一化和评分
internal/run      单任务调度、取消和进度事件
internal/publish  输出格式、发布客户端和串行队列
internal/storage  设置、凭据、数据源和最近历史
frontend/src/features
```

## 致谢

本项目的业务目标和部分配置取舍受到 [xinyitang3/cfnb](https://github.com/xinyitang3/cfnb) 启发。CF Node Bench 没有沿用其 Python 实现，而是使用 Go 与 Wails 重新设计为可取消、可测试的桌面探测流水线。

国家旗帜使用随应用内置的 Twemoji Mozilla 字体离线渲染。第三方字体和图形许可见 `frontend/public/THIRD_PARTY_NOTICES.md`。

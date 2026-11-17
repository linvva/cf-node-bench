# 开发指南

[返回项目首页](../README.md)

## 准备环境

准备 Go、Node.js、pnpm 和 Wails 3 CLI，并安装对应平台的 [Wails 系统依赖](https://v3.wails.io/getting-started/installation/)。Linux 使用 GTK4 / WebKitGTK 6.0，macOS 构建需要 Xcode Command Line Tools。

环境和依赖要求以仓库配置为准：Go 与 Wails 依赖见 [`go.mod`](../go.mod)，包管理器见 [`package.json`](../package.json)，前端依赖见 [`frontend/package.json`](../frontend/package.json)，构建环境见 [Workflow](../.github/workflows/release.yml)。安装依赖时使用锁文件。

在仓库根目录安装与项目依赖匹配的 Wails CLI：

```bash
go install github.com/wailsapp/wails/v3/cmd/wails3@$(go list -m -f '{{.Version}}' github.com/wailsapp/wails/v3)
pnpm install --frozen-lockfile
wails3 generate bindings -ts -i -clean=true
```

## 启动开发

```bash
wails3 task dev
```

只预览前端时运行：

```bash
pnpm dev
```

浏览器预览使用 mock 桥接，不进行真实测速或外部发布；桌面能力需通过 Wails 验证。

## 检查与构建

```bash
go test ./...
pnpm test
pnpm test:relay
pnpm typecheck
pnpm test:e2e
pnpm build
wails3 build
```

首次运行浏览器检查前安装 Chromium：

```bash
pnpm --dir frontend exec playwright install chromium
```

生成的 `frontend/bindings` 不入库，首次类型检查或浏览器构建前需运行上述生成命令。Go API 变更后重新生成；`wails3 build` 会自动生成并构建前端。Linux 依赖包应与项目 [Workflow](../.github/workflows/release.yml) 保持一致。

在对应平台的构建环境中指定系统和架构，`GOARCH` 可选 `amd64` 或 `arm64`：

```bash
# Windows
wails3 build GOOS=windows GOARCH=amd64

# Linux
wails3 build GOOS=linux GOARCH=amd64

# macOS；每次构建一个架构，再组装 .app
wails3 build GOOS=darwin GOARCH=arm64
wails3 task bundle:darwin
```

产物位于 `build/bin`。发布构建通过 `VERSION=<版本号>` 和 `UPDATE_PUBLIC_KEY=<公钥>` 注入版本及更新公钥，具体参数与命名由 [Workflow](../.github/workflows/release.yml) 定义。

自动化测试使用本地服务与演示数据，不以公网节点可用性作为固定断言。实际网络验收可显式运行 H3 检查，候选通过环境变量传入：

```bash
CF_NODE_H3_SMOKE_WORKER=worker.example.com \
CF_NODE_H3_SMOKE_CANDIDATES='203.0.113.1:443 edge.example.com:8443' \
go test -v ./internal/run -run '^TestH3NetworkSmoke$' -count=1
```

`CF_NODE_H3_SMOKE_WORKER` 可省略以使用公共验证目标；`CF_NODE_H3_SMOKE_FULL=1` 启用完整下载测量。`CF_NODE_H3_SMOKE_EXPECT` 与 `CF_NODE_H3_SMOKE_REJECT` 可分别指定应通过和应淘汰的候选。

## GitHub Release 自更新

应用启动后检查一次稳定版，仅在用户点击后下载；SHA-256 和 Ed25519 签名验证通过后，等待用户重启安装。测速和发布任务进行时禁止更新重启。原有配置与 Windows WebView2 数据目录保持不变。更新要求程序所在目录可写；应用不会自动提权。

首次启用发布流程前，在受控环境生成密钥：

```bash
go run ./cmd/updatemanifest -keygen .update-keys
```

将 `.update-keys/private.key` 内容存为仓库 Actions Secret `UPDATE_SIGNING_KEY`，将 `public.key` 内容存为 Actions Variable `UPDATE_PUBLIC_KEY`。私钥离线备份，不提交或输出到日志；公钥会编入应用。两者不匹配或应用文件缺失时，发布流程失败。`.update-keys` 已被 Git 忽略。后续版本沿用同一密钥，轮换前需先设计旧版客户端的信任迁移。

Tag Workflow 为 Windows、Linux、macOS 各发布 `amd64` 和 `arm64` 两种架构，共 6 个应用文件。文件前缀为 `CF-Node-Bench-<tag>-`，后缀分别为 `windows-amd64.exe`、`windows-arm64.exe`、`linux-amd64`、`linux-arm64`、`macos-amd64.zip`、`macos-arm64.zip`。Windows 和 Linux 发布原始可执行文件，macOS ZIP 顶层仅包含对应架构的 `CF-Node-Bench.app`。

手动下载与自更新使用同一文件。Release 另附 `update-manifest.json` 和 `SHA256SUMS.txt`，合计 8 个附件。清单包含各应用文件的 SHA-256 和 Ed25519 签名；客户端从 GitHub 的 latest Release 获取清单，再下载清单中指定版本和架构的文件。预发布不进入稳定版更新通道。许可证随前端嵌入应用，可在“设置”中离线查看。

开发构建或未嵌入公钥时，更新界面显示不可用。不含自更新功能的旧版需要先手动下载安装。更新签名与 Windows Authenticode / Apple Developer ID 签名是独立机制。

## 项目结构

```text
internal/source    数据源获取、本地文件读取与解析
internal/probe     连通性、HTTP、QUIC 与下载测量
internal/ranking   门槛、归一化与评分
internal/run       测速流水线、检查点、取消与调度
internal/publish   输出格式与发布渠道
internal/storage   设置、凭据、历史与检查点存储
frontend/src       页面、组件、状态与桌面桥接
```

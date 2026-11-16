# 开发指南

[返回项目首页](../README.md)

## 准备环境

准备 Go、Node.js、pnpm 和 Wails CLI，并安装对应平台的 [Wails 系统依赖](https://wails.io/docs/gettingstarted/installation/)。

环境和依赖要求以仓库配置为准：Go 与 Wails 依赖见 [`go.mod`](../go.mod)，包管理器见 [`package.json`](../package.json)，前端依赖见 [`frontend/package.json`](../frontend/package.json)，构建环境见 [Workflow](../.github/workflows/release.yml)。安装依赖时使用锁文件。

在仓库根目录安装与项目依赖匹配的 Wails CLI：

```bash
go install github.com/wailsapp/wails/v2/cmd/wails@$(go list -m -f '{{.Version}}' github.com/wailsapp/wails/v2)
pnpm install --frozen-lockfile
```

## 启动开发

```bash
wails dev
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
wails build
```

首次运行浏览器检查前安装 Chromium：

```bash
pnpm --dir frontend exec playwright install chromium
```

Linux 使用 WebKitGTK 构建时，依赖包与构建标签应与项目 [Workflow](../.github/workflows/release.yml) 保持一致。发布构建流程也由该文件定义。

自动化测试使用本地服务与演示数据，不以公网节点可用性作为固定断言。实际网络验收可显式运行 H3 检查，候选通过环境变量传入：

```bash
CF_NODE_H3_SMOKE_WORKER=worker.example.com \
CF_NODE_H3_SMOKE_CANDIDATES='203.0.113.1:443 edge.example.com:8443' \
go test -v ./internal/run -run '^TestH3NetworkSmoke$' -count=1
```

`CF_NODE_H3_SMOKE_WORKER` 可省略以使用公共验证目标；`CF_NODE_H3_SMOKE_FULL=1` 启用完整下载测量。`CF_NODE_H3_SMOKE_EXPECT` 与 `CF_NODE_H3_SMOKE_REJECT` 可分别指定应通过和应淘汰的候选。

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

# Agent Master

**简体中文** · [English](README.en.md)

在自己的机器上运行 Claude Code，并从浏览器或桌面端管理会话。Agent Master 在每台机器上安装一个轻量守护进程，内置 Web 界面，实时推送 Claude 的消息与工具调用。

- 无中心服务器，无需托管账号
- 一个客户端管理多台机器
- 会话可恢复，支持 token 级流式输出
- 内置 Web 界面，可选 Electron 桌面端
- 每台机器独立的访问令牌与工作目录限制

> Agent Master 会以守护进程的权限读取、修改和执行文件。请将其部署在可信的内网环境（例如 Tailscale），不要把 `8888` 端口直接暴露到公网。

## 工作方式

在每台需要运行 Claude Code 的机器上安装守护进程。之后既可以直接打开该机器内置的 Web 界面，也可以从桌面端同时连接多台机器。

```text
浏览器 / 桌面端
      │  HTTP + SSE
      ▼
Agent Master 守护进程  ──►  Claude Code CLI
      │
      └── 会话、配置与日志存放于 ~/.agent-master/
```

没有中心枢纽。每个客户端自行保存机器地址与令牌列表，并直连这些机器。

## 快速开始

### 前置条件

在每台运行守护进程的机器上：

1. 安装并登录 Claude Code（`claude`）。
2. 使用 npm 安装方式时，需要 Node.js 20 或更高版本。

### 1. 安装并启动守护进程

```bash
npm install -g @ken-chy129/agent-master
agent-master start
```

`start` 会创建一个用户级后台服务，并输出 Web 地址与访问令牌。守护进程本身是独立的 Go 二进制文件，Node.js 仅用于安装器和命令入口。

### 2. 打开 Web 界面

访问：

```text
http://127.0.0.1:8888
```

粘贴 `agent-master start` 输出的令牌即可。生产版 Web 应用已内置在守护进程中，无需单独部署。

从其他设备连接时执行：

```bash
agent-master pair
```

该命令会输出可用地址、令牌、深链接与二维码。

### 3. 可选：桌面端

从 [最新 Release](https://github.com/Ken-Chy129/agent-master/releases/latest) 下载桌面客户端：

- macOS：选择匹配 Apple Silicon（`arm64`）或 Intel（`x64`）的 `.dmg`
- Windows：选择带版本号的桌面安装包 `.exe`，而非 `agent-master-windows-*` 运行时二进制

桌面端只是客户端，不能替代运行 Claude Code 的机器上的守护进程。它通过 Electron `safeStorage` 保存令牌，并支持 `agentmaster://` 配对链接。桌面端与 Web 界面可同时使用。

桌面端目前未签名。Windows 可能出现 SmartScreen 提示。macOS 上若应用被拦截，执行一次以清除下载隔离标记：

```bash
xattr -cr "/Applications/Agent Master.app"
```

## 多机管理

在每台目标机器上运行 Agent Master，然后用 `agent-master pair` 的输出逐台添加。若需在本地网络之外访问，请将守护进程所在机器与客户端设备接入同一 Tailscale 网络，并把该网络地址设为 `public_url`。

不要将 `8888` 端口转发到公网。若确实无法避免公网暴露，请使用 TLS 反向代理、足够强的令牌，并限制 CORS 来源。

## 命令

| 命令 | 用途 |
| --- | --- |
| `agent-master start` | 安装或更新后台服务并启动 |
| `agent-master status` | 查看服务与健康状态 |
| `agent-master doctor` | 诊断"看起来已启动但会话失败"的原因，并给出修复方式 |
| `agent-master pair` | 输出连接地址、令牌、深链接与二维码 |
| `agent-master token` | 输出当前访问令牌 |
| `agent-master restart` | 重启已安装的服务 |
| `agent-master stop` | 停止服务但不移除 |
| `agent-master uninstall` | 移除服务定义，保留数据与配置 |
| `agent-master serve` | 前台运行，用于开发或调试 |
| `agent-master version` | 输出已安装的版本号 |

## 排查问题

当 `start` 或 `status` 显示正常、但会话仍然失败时，执行：

```bash
agent-master doctor
```

它会逐项检查会话执行所依赖的前置条件——守护进程是否在服务、端口是否被残留进程占用、`claude` 可执行文件是否存在、凭证是否可用、登录 shell 环境是否成功导入——并对每一项失败给出可直接执行的修复命令，同时输出日志路径与近期错误。存在故障时退出码为 `1`，可用于脚本或 CI 检查。

需要注意：守护进程"正在运行"与"可以执行会话"是两件事。`/health` 同时给出 `status`（进程是否在服务）与 `ready`（会话是否有望成功执行），客户端应依据后者判断可用性。

## 更新与卸载

更新 npm 安装：

```bash
npm install -g @ken-chy129/agent-master@latest
agent-master restart
```

从原生安装方式切换到 npm 时，请先移除旧服务。此操作不会删除会话、配置与令牌：

```bash
agent-master uninstall
npm install -g @ken-chy129/agent-master
agent-master start
```

完全移除 npm 安装：

```bash
agent-master uninstall
npm uninstall -g @ken-chy129/agent-master
```

若使用 NVM 管理 Node.js，全局 npm 包归属于当前激活的 Node.js 版本，切换版本后可能需要重新安装。

## 原生安装方式

原生安装器将最新的守护进程放置在 `~/.local/bin`，无需 Node.js。

Linux 与 macOS：

```bash
curl -fsSL https://raw.githubusercontent.com/Ken-Chy129/agent-master/main/install.sh | bash
agent-master start
```

Windows PowerShell：

```powershell
irm https://raw.githubusercontent.com/Ken-Chy129/agent-master/main/install.ps1 | iex
agent-master start
```

可通过 `INSTALL_DIR` 指定安装位置，或通过 `AGENT_MASTER_VERSION` 锁定特定版本。

## 配置与数据

Agent Master 的状态存放于 `~/.agent-master/`：

- `config.json` —— 守护进程设置与访问令牌
- `agent-master.db` —— 会话事件账本与投影
- `daemon.log` —— 滚动式守护进程日志
- `uploads/` —— 会话中暂存的图片

配置示例：

```json
{
  "host": "0.0.0.0",
  "port": 8888,
  "token": "<自动生成>",
  "claude_bin": "",
  "workspace_roots": [],
  "allowed_origins": [],
  "public_url": "",
  "shell_env_keys": [],
  "shell_env_optional": false
}
```

- `claude_bin`：Claude CLI 的路径；为空时从 `PATH` 解析 `claude`
- `workspace_roots`：允许的会话目录；为空表示当前不做限制
- `allowed_origins`：允许的浏览器来源；为空表示携带有效令牌的任意来源均可
- `public_url`：`pair` 对外公布的地址，例如 Tailscale 或反向代理地址
- `shell_env_keys`：上次成功探测到的登录 shell 变量**名称**（仅名称，不含取值），由守护进程自动维护
- `shell_env_optional`：设为 `true` 时，即使此前可用的 shell 凭证变量不再可读，也继续执行会话

配置文件以仅当前用户可读的权限创建。请将其中的令牌视为机密。

## Release 文件说明

大多数用户只需要 npm 命令，以及可选的一个桌面安装包。GitHub Releases 中的其余文件是 npm 与原生安装器自动使用的运行时资源：

- macOS `.dmg` 与带版本号的 Windows `.exe` —— 桌面客户端
- `agent-master-<os>-<arch>` —— 由安装器选择的守护进程二进制
- `SHA256SUMS` —— 用于校验守护进程下载的校验和清单

## 开发

从源码构建守护进程与内置 Web 界面：

```bash
make build
./dist/agent-master serve
```

运行后端测试与检查：

```bash
go test ./...
go vet ./...
```

以开发模式运行 Web 客户端：

```bash
cd frontend
npm ci
npm run dev -w @agent-master/web
```

构建或运行桌面客户端：

```bash
cd frontend
npm run build -w @agent-master/desktop
npm run dev -w @agent-master/desktop
```

Android 项目包含一个已测试的 Kotlin 核心与 Compose 应用骨架，但 Android 客户端尚未完成构建、验证与发布，目前不作为受支持的客户端。

## 项目结构

```text
cmd/agent-master/        CLI 与守护进程入口
internal/config/         配置与本地路径
internal/service/        systemd、launchd 与 Windows 服务集成
internal/provider/       Claude Code CLI 驱动与凭证探测
internal/shellenv/       登录 shell 环境解析
internal/server/         HTTP API、SSE 与内置 Web 界面
internal/session/        会话执行与事件投影
internal/store/          SQLite 事件账本
frontend/apps/web/       React Web 客户端
frontend/apps/desktop/   Electron 桌面客户端
frontend/packages/core/  共享的 TypeScript API 客户端与模型
npm/agent-master/        npm 安装器与原生命令入口
android/                 实验性 Android 客户端
```

## 文档

- [HTTP API](docs/API.md)
- [架构与设计](docs/DESIGN.md)
- [开发者交接与实现状态](docs/HANDOFF.md)

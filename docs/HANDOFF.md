# agent-master — 交接文档

> 快照日期：项目已完成 M0–M6 + 逐字流式 + 服务端 render_state（三端）。
> 仓库：`github.com/Ken-Chy129/agent-master`，主分支 `main`。
> 本文档面向接手者，说明「是什么、怎么跑、做了什么、还差什么、有哪些坑」。
> 配套文档：架构设计见 [DESIGN.md](DESIGN.md)，HTTP/SSE 契约见 [API.md](API.md)。

---

## 1. 项目是什么

一套「**一个控制端管很多台机器上的 AI Agent**」的系统。

- 每台开发机/服务器跑一个 **Go 守护进程（daemon）**，用子进程方式驱动本机已登录的 **Claude Code**（`claude` CLI），做会话管理并暴露 HTTP+SSE API。
- 控制端（办公 Mac 的 **Electron 桌面 App** / 浏览器 **Web** / **Android**）持有一份「机器清单」，直连每台 daemon、随时切换。**没有中心 hub**，每台机器一个独立 token。
- 典型场景：办公 Mac 上管控各台机器的 agent；离开电脑时用手机继续。外网可达性用 **Tailscale**（对 daemon 透明，零公网暴露）。

参考对象是 Rust 项目 Garyx；本项目取其架构思想（事件账本、投影、服务端 render_state、多端一致），换成 Go + TS + Kotlin 技术栈。

---

## 2. 技术栈

| 层 | 选型 |
|---|---|
| 后端 daemon | **Go 1.25**（单静态二进制，`CGO_ENABLED=0`） |
| 驱动 Claude | `os/exec` 子进程 + `--output-format/--input-format stream-json`（不重写 agent，只解析协议） |
| 存储 | SQLite via **`modernc.org/sqlite`（纯 Go，无 cgo）** —— 才能出真·单静态二进制 |
| HTTP | 标准库 `net/http`（Go 1.22 路由）+ SSE |
| 前端(Web+桌面) | **React + Vite + TS**，状态 Zustand；npm workspaces |
| 桌面壳 | **Electron**（`safeStorage` 存 token、`agentmaster://` 深链） |
| 移动 | **Kotlin + Jetpack Compose**；`:core` 为纯 JVM 库（可 JUnit 测） |
| 组网 | **Tailscale**（推荐；也支持反代 + `public_url`） |
| 二维码/配对 | `mdp/qrterminal`（`agent-master pair` 打印终端二维码） |

代码量：Go ~2.8k 行，TS/TSX ~2k 行，Kotlin ~2.7k 行。

---

## 3. 仓库结构

```
agent-master/
├── cmd/agent-master/        # CLI 入口：start/stop/restart/status/uninstall + pair/token + serve + version（service <sub> 为兼容别名）
│   ├── main.go              #   命令分发 + serve（组装 store/provider/session/server）
│   └── pair.go              #   pair 命令（URL/token/深链/二维码）
├── internal/
│   ├── config/              # ~/.agent-master/config.json（host/port/token/claude_bin/workspace_roots/allowed_origins/public_url）
│   ├── store/               # SQLite：会话/事件账本/runs/recent 投影/intents；queries.go 是数据访问；有单测
│   ├── provider/            # Provider 接口 + Claude 驱动（子进程 stream-json，含逐字 delta 解析）
│   ├── session/             # 会话编排：service.go（发消息/中断/账本写入/投影）+ broadcast.go（SSE 帧广播：事件/delta）
│   ├── render/              # ★ 服务端 render_state：Compute([]Event)→RenderState 纯函数 reducer；有单测
│   ├── server/              # HTTP：server.go(路由) auth.go cors.go sessions.go stream.go(SSE+render) workspaces.go；有 httptest 单测
│   ├── service/             # systemd(Linux)/launchd(macOS)/Run键自启(Windows) 服务安装
│   ├── webui/               # Vite 生产产物的 Go embed + SPA 静态处理器
│   └── version/             # 构建版本（ldflags 注入）
├── frontend/                # npm workspaces
│   ├── packages/core/       # @agent-master/core（纯 TS：ApiClient/SseClient/types/machines）—— 三端契约的 TS 参考实现
│   ├── apps/web/            # @agent-master/web（React：机器切换/会话列表/目录选择/会话流；哑渲染 render_state）
│   └── apps/desktop/        # @agent-master/desktop（Electron：main.ts/preload.ts，加载同一份 Web UI）
├── android/                 # Gradle 多模块
│   ├── core/                # 纯 Kotlin/JVM：Models/ApiClient/SseClient/SessionStore（对齐 TS core）；34 单测
│   └── app/                 # Compose App：MainActivity/ViewModel/ConversationScreen/EncryptedMachineStore/深链
├── docs/                    # DESIGN.md（架构）/ API.md（契约）/ HANDOFF.md（本文件）
├── install.sh               # GitHub Release 安装脚本（Linux/macOS）
├── install.ps1              # GitHub Release 安装脚本（Windows PowerShell）
├── npm/agent-master/        # npm 全局安装器（下载并校验对应平台原生二进制）
├── Makefile                 # Web embed + build / release(交叉编译6平台) / test / vet
└── README.md
```

---

## 4. 核心概念

- **Session（会话）**：一次持续对话。绑定 `provider`（当前恒 claude）、`workspace_dir`（工作目录，启动后固定）、`native_session_id`（Claude 原生 session，用于 `--resume` 续接）、模型等。
- **Run（运行）**：一次「用户发一条 → agent 跑一轮」，有 `run_id` 和状态。
- **Event（事件）**：append-only 账本一条，带会话内单调递增 `seq`。类型：`user_message` / `assistant_message` / `tool_call` / `tool_result` / `run_started` / `run_finished` / `error`。
- **Projection（投影）**：读优化派生表（`recent_sessions` 列表、`intents` 幂等）。**写时更新，读路由不回算**。
- **render_state（服务端渲染态）**：由 `internal/render` 把账本折叠成「最终该显示的行结构」（`rows` + `tailActivity` + `lastRunState`）。工具调用与结果合并成一行、run 事件折进状态。**客户端哑渲染，不再各自推导**。
- **写入铁律**：先 append event 拿 seq → 更新投影 → 再广播。

### SSE 三种帧（`GET /api/sessions/:id/stream`）
| 帧 | 用途 | 是否可续传 |
|---|---|---|
| `am_event` | 已提交事件（带 seq）——驱动**续连游标** + 兼容旧客户端 | 是（`Last-Event-ID`/`after_seq`） |
| `am_render` | 服务端 render_state 快照——**客户端据此渲染** | 每次提交后重发；不用续传 |
| `am_delta` | 逐字 token 片段——**实时预览** | 否（临时，不入账本） |

完整契约见 [API.md](API.md)。

---

## 5. 发消息数据流

```
Client            Go daemon                          claude CLI(子进程)
 │ POST /send      │                                  │
 │────────────────▶ append user_message + run_started │
 │ 202 {runId}     │ 广播 am_event + am_render         │
 │◀────────────────│── exec claude(-p,resume,dir,     │
 │ (在 /stream 订阅) │      --include-partial-messages) ▶ 拉起/续接
 │ am_render(user) │◀ system(session_id) 存 native_id  │
 │ am_delta ...    │◀ stream_event text_delta ─────────│  实时预览(不入账本)
 │ am_render(asst) │◀ assistant / tool_use / result ──│  提交 → 重算 render_state
 │ am_render(done) │ append run_finished, 更新投影      │  cmd.Wait()
```

---

## 6. 已完成（含验证程度）

提交历史即里程碑（`git log --oneline`）：

| 提交 | 里程碑 | 内容 | 验证 |
|---|---|---|---|
| `cc107b8` | **M0** 骨架 | Go CLI + config + SQLite schema + `/health` + token 中间件 + service 安装 | 本机 |
| `f722212` | **M1** Claude 打通 | Provider 子进程 stream-json + 事件账本 + `/send` + SSE `/stream` + resume | 本机（真实 claude E2E） |
| `ed652e6` | **M2** 后端打磨 | 分页 hasMore、投影 last_seq、持久化 intent 幂等、CORS、store 单测 | 本机 |
| `b3be13d` | **M3** Web | `@agent-master/core`（ApiClient/SseClient）+ React 会话界面 | typecheck/build + core E2E |
| `33faffc` | **M4** 多机+桌面 | 机器清单切换 + Electron 壳（safeStorage/深链）+ `pair` 前置 | 构建/编译；GUI 需 Mac |
| `3c4f0d4` | **M5** 打磨 | `/api/workspaces` 目录浏览 + Web 目录选择弹窗 + 重连指示 + server httptest | 本机 |
| `c719cc9` | **M6** Android | Gradle `:core`（Kotlin/JVM，28测试）+ Compose `:app` 脚手架 | `:core` 本机；`:app` 需 Mac |
| `25522d9` | 逐字流式 | `--include-partial-messages` → `am_delta`；Web 预览气泡 | 本机（curl 实测流式） |
| `a608f8f` | **render_state** | `internal/render` reducer + `am_render` 帧 + `/render` 接口；Web 切哑渲染 | 本机（reducer 单测 + E2E RENDER_OK） |
| `682d355` | Android render_state | Android `:core` 消费 render_state + delta（34 测试），`:app` 同步 | `:core` 本机；`:app` 需 Mac |

**验证矩阵**

| 模块 | 本机验证 | 需在 Mac/设备验证 |
|---|---|---|
| Go daemon（会话/账本/SSE/render/pair/service） | ✅ 单测 + 真实 claude E2E | — |
| Go daemon（Windows：Run键自启/pidfile stop/CREATE_NO_WINDOW） | ✅ 交叉编译 + vet | ❗ 全部行为需 Windows 实机验证（本环境无 Windows） |
| Web（React 哑渲染 + 多机 + 目录选择 + 流式） | ✅ typecheck/build + core E2E | 浏览器目视 |
| 桌面（Electron） | ✅ 编译（tsc） | GUI 启动 + `.dmg` 打包（macOS 专属）；Windows NSIS 包由 CI 出，需实机目视 |
| Android `:core` | ✅ `gradle :core:test` 34 绿 | — |
| Android `:app`（Compose UI） | ❌（本机无 Android SDK） | Android Studio 构建运行 |

---

## 7. 如何构建 / 运行 / 测试

**前提**：跑 daemon 的机器需装 `claude` 并已登录（`claude` 在 PATH，或 config 里 `claude_bin` 指定）。

### daemon + 内嵌 Web（Go + Node 20）
```bash
make build                    # 构建 Vite UI 并嵌入 dist/agent-master
make test                     # go test ./...（store/server/render 单测）
make release                  # 交叉编译 linux/darwin/windows × amd64/arm64 + sha256
./dist/agent-master serve     # 前台运行；API + Web UI 监听 :8888
./dist/agent-master token     # 打印本机 token
./dist/agent-master pair      # 打印 URL/token/深链/二维码
./dist/agent-master start             # 装成后台服务并启动（systemd/launchd/Windows Run键；stop/restart/status/uninstall 同理）
```

生产 Web 直接访问 `http://127.0.0.1:8888`。开发前端仍可单独运行：

### 前端 Web 开发
```bash
cd frontend && npm install
npm run typecheck             # 三 workspace tsc
npm run build -w @agent-master/web
npm run dev   -w @agent-master/web    # Vite http://localhost:5173
# 端到端冒烟（需 daemon 在跑）：node packages/core/scripts/smoke.mjs（AM_BASE_URL/AM_TOKEN）
```

### 桌面（Electron，Mac）
```bash
cd frontend
npm run dev:web  -w @agent-master/desktop   # 终端A：Web dev server
npm run dev      -w @agent-master/desktop   # 终端B：Electron
npm run dist     -w @agent-master/desktop   # 打包 .dmg（须在 macOS）
```

### Android（Mac + Android Studio）
```bash
# 需 JDK 21 + Android Studio + SDK Platform 34
cd android
./gradlew :core:test          # 纯 Kotlin 单测（本机也能跑，34 绿）
./gradlew :app:assembleDebug  # 或 :app:installDebug（需 Android SDK）
```

---

## 8. 部署与网络

- **推荐**：每台机器 `curl install.sh | bash`（Linux/macOS，装到 `~/.local/bin`，免 sudo；Release 已发 v0.1.0）/ `irm install.ps1 | iex`（Windows），或 `make build` 后 `agent-master start`；把机器 + 你的设备加进同一 **Tailscale tailnet**，客户端用 tailnet 地址（`http://100.x.x.x:8888`）添加机器。Tailscale 对 daemon 透明，零代码、零公网暴露。
- **鉴权**：单 Bearer token（`crypto/subtle` 常量时间比较），首启自动生成存 `~/.agent-master/config.json`（0600）。Docker 端口映射下不启用「同机免密」，统一用 token。
- **Docker（可选）**：镜像内需装 `claude` 并挂 `~/.claude` 凭据 + 挂工作目录 + 挂 SQLite 卷（详见 DESIGN.md §9）。原生二进制部署更省事（直接用本机 claude 登录）。

---

## 9. 待做 / 后续路线

**近期可做（按价值）**
1. **Android `:app` 在 Mac 上首次构建**：验证 Compose UI 真跑起来（`:core` 已测试，`:app` 是脚手架）。
2. **桌面/Web 浏览器目视验证**：确认 UI 渲染、多机切换、目录选择、流式预览的实际效果。
3. **首次发布 npm 包**：在 GitHub Actions 配置 `NPM_TOKEN` 后打新 tag；流水线会发布 `@ken-chy129/agent-master`（命令仍为 `agent-master`），未配置 token 时仍会把 `.tgz` 附到 GitHub Release。
4. **Codex provider**：实现 `provider.Provider` for `codex`（`codex app-server` JSON-RPC）。**注意本机无 codex CLI，无法验证**——需在有 codex 的机器上做。`internal/provider` 接口已预留。

**render_state / 体验增强**
5. 按**用户轮次分组**（一次提问 + 其引发的多步 工具/回复 收成一组），当前是扁平行列表。
6. **thinking 尾部活动**（把 `thinking_delta` 也流式化，或在 render_state 里体现「思考中」）。
7. 长会话 render 的**窗口化/增量重算**（当前每次提交从内存全量 `Compute`，见 `renderCap=2000` 上限；`store.EventsAfter/EventsBefore` 已有分页可用）。

**扩展（架构已预留，非必须）**
8. 其它 provider（Gemini 等）、多渠道机器人（Telegram/飞书/Discord）、Agent 团队、workflow、automation/定时任务、工具审批（`canUseTool` → 手机审批）。

---

## 10. 已知限制 / 坑（务必读）

- **claude 必须在 daemon 机器登录**：daemon 只是驱动本机 `claude`；未登录则 run 会报错。
- **不是沙箱**：claude 以 daemon 进程权限读写工作目录、执行命令——这是高权限远程通道，务必只在可信网络（Tailscale）暴露，token 保管好。
- **`workspace_roots` 白名单默认空 = 不限制**：任意目录可作工作区（防目录穿越只在配置了 roots 时生效）。生产建议配置 roots。
- **SQLite 必须用纯 Go 驱动 + `CGO_ENABLED=0`**：否则单静态二进制/交叉编译会坏。`store.go` 单连接串行写。
- **本环境网络**：`github.com` 资产下载/`services.gradle.org` 被墙；**能用的镜像**：`goproxy.cn`（Go）、`npmmirror.com`（npm/Electron）、`maven.aliyun.com` + 腾讯 gradle 镜像（Android，已配在 `android/settings.gradle.kts` + wrapper）。SSH 走 `ssh.github.com:443`（22 端口不通，见 `~/.ssh/config`）。
- **Android `:app` 从未在本环境编译**（无 Android SDK）——首次 Mac 构建可能需要小修（版本/依赖），`:core` 已测试无虞。
- **逐字流式粒度由 claude 决定**：短回答常一个 delta 到位，长回答才明显逐字。
- **Windows 的差异（`service_windows.go` / `claude_proc_windows.go`）**：
  - 后台方式是 **HKCU Run 键自启 + detached 进程**，不是真服务——`schtasks` 的登录触发器和 Windows 服务都要管理员权限，且服务跑在用户 profile 之外（claude 登录态在用户 profile 里）。代价：无 crash 自动重启。
  - 自启命令用 `conhost.exe --headless` 包一层保证无窗口，需要 **Windows 10 1903+**。
  - `stop` 靠 daemon 写的 `~/.agent-master/daemon.pid` + `taskkill /T /F`（隐藏控制台进程收不到优雅关闭；SQLite journal 保证安全，但会连带杀掉进行中的 claude run）。
  - **中断 run 在 Windows 上是硬杀**（无法跨控制台发 Ctrl+C），claude 转写是增量落盘的，最坏丢被中断 run 的尾巴。
  - claude 探测优先 `%USERPROFILE%\.local\bin\claude.exe`（原生安装器），npm 的 `claude.cmd` 垫片放最后——cmd.exe 对含特殊字符的参数（聊天消息很常见）会出错，推荐用原生安装。
  - **以上均未在 Windows 实机验证过**（本环境只有 Linux，只做了交叉编译 + vet）。
- **render 全量重算**：`renderCap=2000` 事件上限；超长会话需做增量（见待做 #7）。
- **git 提交约定**：作者 `Ken-Chy129 <ken-chy129@qq.com>`；`dist/`、`node_modules/`、`android` 的 `build/`.gradle/` 均已 gitignore，勿提交产物。

---

## 11. 关键文件速查

| 想改什么 | 看哪里 |
|---|---|
| 加/改 HTTP 接口 | `internal/server/server.go`(路由) + `sessions.go`/`stream.go`/`workspaces.go` |
| 改渲染结构（行/分组/状态） | `internal/render/render.go`（改这一处，三端自动一致）+ `render_test.go` |
| 驱动 claude 的参数/协议解析 | `internal/provider/claude.go` |
| 会话/账本/发消息编排 | `internal/session/service.go` + `broadcast.go` |
| 数据库表/查询 | `internal/store/store.go`(schema) + `queries.go` |
| 加新 provider（codex 等） | 实现 `internal/provider/provider.go` 的 `Provider` 接口 |
| 前端契约类型 | `frontend/packages/core/src/types.ts`（改后三端对照更新） |
| Web 状态/渲染 | `frontend/apps/web/src/store.ts` + `components/Conversation.tsx` |
| Android 契约/状态 | `android/core/src/main/kotlin/com/agentmaster/core/{Models,SseClient,SessionStore}.kt` |
| CLI 命令 | `cmd/agent-master/main.go` + `pair.go` |
| 配置项 | `internal/config/config.go` |

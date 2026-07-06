# agent-master — 架构设计文档（v1.0 最终版）

> 一套「一个控制端管很多台机器」的 AI Agent 控制系统。每台开发机/服务器跑一个
> **Go daemon**，驱动本机的 **Claude Code**（后续可扩展 Codex 等）；办公 Mac 上的
> **Electron 桌面 App**（+ Web 端）+ 未来的 **Android(Kotlin)** 作为遥控端，随处管控。
> 网络够得着用 **Tailscale**。参考对象：Garyx（Rust），本设计取其架构思想、换成你的技术栈。

**技术栈定稿**：后端 Go / 桌面 Electron+React / 移动 Kotlin / 存储 SQLite / 组网 Tailscale。

---

## 1. 目标与范围

### v1 核心闭环
- 每台机器一个 Go daemon，**子进程方式驱动 Claude Code**（`Provider` 接口预留 Codex，v1 只实现 Claude）。
- **会话管理**：持久化、列出、续接(resume)、中断、删除。
- **流式**：Claude 输出实时推给客户端；断线可续传。
- **多机管理**：客户端存一份「机器清单」，直连每台 daemon，一键切换。
- **鉴权**：每台机器独立 token。
- **桌面端 App（MVP，优先级最高）** + 顺带 **Web 端**（同一份前端）。
- **部署简单**：单静态二进制 + `service install`（systemd/launchd），一台机器两行命令起。

### v1 先不做（架构预留，后续按需补）
- 其它 provider（Codex/Gemini）——加 `Provider` 实现即可。
- 服务端渲染态 `render_state`——v1 客户端直接渲染 event 流；多端稳定后再上。
- 多渠道机器人 / Agent 团队 / workflow / automation / 定时任务 / 工具审批拦截。
- Android——v1 之后做，复用同一 HTTP+SSE 契约。

---

## 2. 总体架构与多机拓扑

**没有中心 hub**（学 Garyx）：每台机器各跑一个 daemon，客户端持有机器清单、直连切换。

```
        ┌─────────────── 遥控端（你带着走）───────────────┐
        │  办公 Mac：Electron 桌面 App     手机：Android    │
        │  浏览器：Web 端                                   │
        │  ── 都内置「机器清单」{名字,URL,token} 切换 ──     │
        └───────────────────────┬────────────────────────┘
                                 │  REST(CRUD) + SSE(流) + Bearer token
                 ┌───────────────┼───────────────┐   （经 Tailscale 内网直连）
                 ▼               ▼               ▼
        ┌─────────────┐ ┌─────────────┐ ┌─────────────┐
        │ 开发机 A     │ │ 开发机 B     │ │ 服务器 C     │
        │ agent-master│ │ agent-master│ │ agent-master│  ← Go daemon（每台一个）
        │  daemon(Go) │ │  daemon(Go) │ │  daemon(Go) │
        │   │子进程    │ │             │ │             │
        │   ▼         │ │             │ │             │
        │ claude CLI  │ │ claude CLI  │ │ claude CLI  │  ← 本机已登录的 claude
        │ + 本机代码目录│ │             │ │             │
        │ SQLite(本地) │ │ SQLite(本地) │ │ SQLite(本地) │
        └─────────────┘ └─────────────┘ └─────────────┘
```

daemon 三层（Go 包）：
- **server**：HTTP，REST 增删改查 + SSE 实时流 + token 鉴权中间件。
- **session**：`Session` 为中心，**append-only 事件账本 + 读投影**（学 Garyx 的 ledger + recent 投影）。
- **provider**：`Provider` 接口抹平不同 CLI；管理 `session → 原生 sessionId` 亲和性并广播流事件。v1 只有 ClaudeProvider。

**够得着（Tailscale）**：所有机器 + 你的设备加入同一 tailnet，客户端连 `http://100.x.x.x:PORT` 直达，零公网暴露、自动 NAT 穿透。对 daemon 透明（照常监听即可）。进阶：Go 可用 **tsnet** 库让 daemon 直接作为 tailnet 节点监听，连单独装 Tailscale 都省了（可选）。

---

## 3. 技术选型

| 层 | 选型 | 理由 |
|---|---|---|
| Daemon 后端 | **Go 1.22+** | 单静态二进制一键多机装；goroutine 管子进程+SSE 舒服 |
| 驱动 Claude | **`os/exec` 子进程 + stream-json (NDJSON)** | 不重写 agent，只写薄协议客户端（照搬 Garyx 思路） |
| HTTP | `net/http`(Go1.22 路由) 或 `chi` | 轻；SSE 用原生 flush |
| 存储 | **SQLite via `modernc.org/sqlite`（纯 Go，无 cgo）** | 关键：无 cgo 才能 `CGO_ENABLED=0` 出真·单静态二进制、易交叉编译 |
| 组网 | **Tailscale**（可选 tsnet 内嵌） | 零公网暴露、NAT 穿透、对 daemon 透明 |
| 前端(桌面+Web) | **React + Vite + TS**，状态 Zustand | 一份代码：浏览器=Web 端，Electron 套壳=桌面端 |
| 桌面壳 | **Electron** | 成熟、生态全、可借鉴 Garyx 桌面脚手架 |
| 移动(后续) | **Kotlin + Compose + OkHttp(SSE) + Coroutine/Flow** | 吃你的 Java 底子 |

---

## 4. 核心概念与数据模型

### 4.1 概念
- **Session（会话）**：一次持续对话。绑定 `provider`（v1 恒 claude）、`workspace_dir`（工作目录，**启动后固定**）、`native_session_id`（Claude 原生 session，用于 resume）、模型等。等价 Garyx 的 thread。
- **Run（运行）**：一次「用户发一条 → Agent 跑一轮」，有 `run_id` 和状态。
- **Event（事件）**：账本一条，带会话内单调递增 `seq`。类型：`user_message`/`assistant_delta`/`assistant_message`/`tool_call`/`tool_result`/`run_started`/`run_finished`/`error`。
- **Projection（投影）**：读优化派生表（会话列表/最后预览）。**写时更新，读路由不回算**。

### 4.2 SQLite Schema
```sql
CREATE TABLE sessions (
  id                TEXT PRIMARY KEY,
  title             TEXT,
  provider          TEXT NOT NULL DEFAULT 'claude',
  model             TEXT,
  workspace_dir     TEXT NOT NULL,
  native_session_id TEXT,                    -- Claude 原生 session（resume 用）
  created_at        TEXT NOT NULL,
  updated_at        TEXT NOT NULL,
  archived          INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE events (                        -- append-only，唯一真相源
  session_id  TEXT NOT NULL,
  seq         INTEGER NOT NULL,              -- 会话内单调递增 = SSE 游标
  type        TEXT NOT NULL,
  run_id      TEXT,
  payload     TEXT NOT NULL,                 -- JSON
  created_at  TEXT NOT NULL,
  PRIMARY KEY (session_id, seq)
);
CREATE TABLE runs (
  id TEXT PRIMARY KEY, session_id TEXT NOT NULL,
  state TEXT NOT NULL,                        -- running|done|interrupted|failed
  started_at TEXT NOT NULL, finished_at TEXT, error TEXT
);
CREATE TABLE recent_sessions (               -- 列表投影（读多）
  id TEXT PRIMARY KEY, title TEXT, last_preview TEXT,
  last_seq INTEGER, active_run_id TEXT, updated_at TEXT NOT NULL
);
CREATE INDEX idx_recent_updated ON recent_sessions(updated_at DESC);
```
**写入顺序铁律（学 Garyx write-then-derive）**：先 append event 拿 seq → 更新 runs/recent_sessions 投影 → 再 SSE 广播。绝不先广播后落库。

> seq 生成：`INSERT ... SELECT COALESCE(MAX(seq),0)+1 ...`，用单写协程（channel 串行化每个 session 的写）避免并发竞态。

---

## 5. 进程控制层（Go）

### 5.1 Provider 接口
```go
type RunOptions struct {
    SessionID       string
    Message         string
    WorkspaceDir    string
    Model           string
    NativeSessionID string // 有则 resume，无则新建
}
type StreamEvent struct {
    Kind            string      // assistant_delta|assistant_message|tool_call|tool_result|system|result
    Text            string
    ToolName        string
    ToolID          string
    Input           any
    Output          any
    NativeSessionID string      // system 首帧带回原生 session id
}
type RunResult struct{ NativeSessionID string }

type Provider interface {
    Type() string
    Run(ctx context.Context, o RunOptions, onEvent func(StreamEvent)) (RunResult, error)
    Interrupt(sessionID string) error
    // 预留：AddInput(sessionID, text) 运行中追加
}
```

### 5.2 ClaudeProvider（子进程 + stream-json）
核心用 `os/exec`：
```go
cmd := exec.CommandContext(ctx, claudeBin,
    "-p",                                   // 非交互 print 模式
    "--output-format", "stream-json",       // 输出 NDJSON 事件
    "--input-format", "stream-json",        // 从 stdin 接收流式输入
    "--verbose",
    // 有则续接：
    // "--resume", o.NativeSessionID,
    // "--model", o.Model,
)
cmd.Dir = o.WorkspaceDir                     // ★ 工作目录
stdin, _ := cmd.StdinPipe()
stdout, _ := cmd.StdoutPipe()
cmd.Start()

// 喂一条 user 消息（stream-json 格式），然后关 stdin
writeUserMessage(stdin, o.Message); stdin.Close()

// 逐行读 NDJSON
sc := bufio.NewScanner(stdout)
sc.Buffer(make([]byte, 1024*1024), 16*1024*1024) // 放大缓冲，单行可能很大
for sc.Scan() {
    var msg map[string]any
    json.Unmarshal(sc.Bytes(), &msg)
    switch msg["type"] {
    case "system":    // init：抓 session_id → 存 native_session_id
    case "assistant": // 拆 text delta / tool_use → onEvent(...)
    case "user":      // tool_result 等
    case "result":    // 收尾（usage 等）
    }
}
cmd.Wait() // 正常结束等进程退出，别硬杀（否则毁 --resume）
```
要点（都踩过 Garyx 的坑，见其 docs/agents/claude-sdk.md）：
- **续接**：首轮从 `system(init)` 抓 `session_id` 存库；下轮 `--resume <id>`。
- **权限**：v1 不拦截，不传 permission 相关回调 → claude 用本机自身配置运行（跟你手动跑一样）。
- **正常结束关 stdin 等退出**，别 force-kill（会毁本地 transcript flush → 影响后续 resume）。
- **中断**：`context` cancel + 给进程发信号；`Interrupt()` 走这里。
- ⚠️ **确切 flags 以本机安装的 `claude` 版本为准**，开工先 `claude -p --help` 核对。

### 5.3 ProviderManager
- `map[sessionID] -> {nativeSessionID, activeRun, cancelFunc}`，读写加锁。
- 内部 `Broadcaster`（每 session 一组订阅 channel）：`StreamEvent` → 落成账本 event → 推给 SSE 订阅者。

---

## 6. API 契约

鉴权：除 `/health` 外均需 `Authorization: Bearer <token>`（或 `?token=`）。见 §8。

### 6.1 REST
| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/health` | 存活探测（免鉴权） |
| GET | `/api/info` | 机器名/版本/provider 可用性（客户端"机器清单"展示用） |
| GET | `/api/sessions?limit=&offset=` | 会话列表（读投影） |
| POST | `/api/sessions` | 新建 `{ workspaceDir, model?, title? }` |
| GET | `/api/sessions/:id` | 详情 |
| PATCH | `/api/sessions/:id` | 改标题/模型 |
| DELETE | `/api/sessions/:id` | 删除 |
| GET | `/api/sessions/:id/messages?before_seq=&limit=100` | 历史分页（events 折叠） |
| POST | `/api/sessions/:id/send` | 发消息 `{ message, clientIntentId }` → 触发 run |
| POST | `/api/sessions/:id/interrupt` | 中断当前 run |
| GET | `/api/workspaces` | 列可选工作目录（配置根目录下） |

> `send` 用 `clientIntentId` 幂等去重，避免重发跑两轮。

### 6.2 SSE（实时流）
`GET /api/sessions/:id/stream`，可带 `Last-Event-ID: <seq>` 或 `?after_seq=<seq>` 续传。
- 服务端**先订阅实时广播，再回放 `seq > after_seq` 历史**，重叠按 seq 去重。
- 每条 SSE `id` = event 的 `seq`；每 30s 发注释行 keep-alive。
- Go 实现：`w.Header().Set("Content-Type","text/event-stream")`，每写一条 `flusher.Flush()`。
- v1 帧（直接下发标准化 event，暂不做 render_state）：
```
id: 42
event: am_event
data: {"seq":42,"type":"assistant_delta","runId":"r_1","payload":{"text":"..."}}
```

---

## 7. 关键时序：发一条消息
```
Client              Go daemon                     claude CLI (子进程)
  │ POST /send {msg}    │                              │
  │─────────────────────▶ append user_message(seq=n)  │
  │                     │ 更新投影                       │
  │ 202 {runId}         │ append run_started(seq=n+1)  │
  │◀─────────────────────│ exec claude(-p,resume,dir) ─▶ 拉起/续接
  │ (已在 /stream 订阅)  │                              │
  │                     │◀ system(session_id) ─────────│ 存 native_session_id
  │ SSE assistant_delta │◀ assistant delta ────────────│
  │◀─────────────────────│ append+广播 (seq++)          │
  │       ...           │◀ tool_use / tool_result ─────│
  │ SSE result          │◀ result ─────────────────────│
  │◀─────────────────────│ append run_finished, 更新投影 │  cmd.Wait()
```

---

## 8. 鉴权与安全
### 8.1 鉴权（每台机器独立 token）
- daemon 首启生成高熵 token 存 `~/.agent-master/config.json`；`agent-master token` 可打印。
- 客户端每次请求带 `Authorization: Bearer <token>`；Go 用 `crypto/subtle.ConstantTimeCompare` 比较。
- **配对**：`agent-master pair` 打印 `agentmaster://pair?url=..&token=..` 深链/二维码；桌面用 OS 安全存储（keytar/safeStorage）存，Web 存 localStorage，Android 存 EncryptedSharedPreferences。
- 每台机器 token 独立、互不影响，比中心 hub 更安全。

### 8.2 网络暴露
- 用 **Tailscale** → 只在 tailnet 内可达，不暴露公网。daemon 可 bind `0.0.0.0`（仅 tailnet 可路由）或用 tsnet 直接挂 tailnet。
- 若非要公网：反代 + TLS + 强 token（不推荐，攻击面大）。

### 8.3 运行权限 & 目录白名单
- v1 **不做审批拦截**：claude 以 daemon 进程权限，按本机自身 permissionMode 运行（= 你手动跑 claude）。这是高权限通道，务必只在可信网络暴露。
- `POST /api/sessions` 的 `workspaceDir` 必须落在配置允许根目录内，防目录穿越。
- 日志/产物不落真实 token、路径、密钥。

---

## 9. 部署（单二进制 + service install，学 Garyx）
每台机器只需：
```bash
# 1) 装二进制（release 下载脚本 或 go install）
curl -fsSL https://.../install.sh | bash        # 放到 /usr/local/bin/agent-master
# 2) 装成后台服务并启动
agent-master service install                     # 写 systemd(Linux)/launchd(macOS) 用户服务
agent-master token                              # 拿 token 填进客户端
```
- **claude 必须已装且已登录本机**（原生跑 → 直接用本机 `~/.claude`，无需 Docker 挂凭据，这是原生比 Docker 省事的地方）。
- 构建：`CGO_ENABLED=0 go build`（配合 modernc.org/sqlite 出真·静态二进制）；`GOOS/GOARCH` 交叉编译各平台。
- Docker 可选：想容器化再提供 Dockerfile（需在镜像内装 claude + 挂 `~/.claude` + 挂工作目录，见旧版 §9 注意事项）。

---

## 10. 前端结构（桌面 + Web 一份代码）
```
frontend/
  packages/core/    纯 TS（无 DOM）
    ├ ApiClient      REST 封装 + token 注入 + 重试
    ├ SseClient      SSE 订阅，断线带 Last-Event-ID 重连
    ├ machines       机器清单 profile 存取/切换（每个={id,name,baseURL,token,headers}）
    ├ models         Session/EventDto/RunState
    └ store(Zustand) machineList / sessionList / conversation（收 SSE→UI 列表）
  packages/ui/      React 组件（SessionList / Conversation / Composer / MachineSwitcher）
  apps/web/         Vite 入口（= Web 端）
  apps/desktop/     Electron（= 桌面端）
    ├ 加载同一份 UI
    ├ token 安全存储（safeStorage）
    ├ agentmaster:// 深链配对
    └ 多机切换 UI（对标 Garyx GatewayProfile 切换器）
```

---

## 11. Android（M6+，复用同一契约）
```
:core (纯 Kotlin)  RelayClient(OkHttp) / SseClient(Flow) / models / machineStore(DataStore+EncryptedPrefs) / state
:app  (Compose)    MachineSwitcher / SessionList / Conversation / Composer
```
业务逻辑全进 `:core` 写单测；UI 只组合（学 Garyx「逻辑进 Core、UI 只组合」）。

---

## 12. 目录结构（monorepo，参考 Garyx 分层）
```
agent-master/
  cmd/agent-master/     CLI 入口：serve / service install / token / pair
  internal/
    server/             HTTP：REST + SSE + auth 中间件
    session/            会话服务 + 事件账本 + 投影
    provider/           Provider 接口 + claude 驱动（codex 后续）
    store/              SQLite（modernc.org/sqlite）
    config/             ~/.agent-master/config.json 读写
    service/            systemd/launchd 安装
  frontend/             见 §10
  android/              见 §11（后续）
  install.sh
  README.md
```

---

## 13. 迭代里程碑（桌面优先）
| 里程碑 | 内容 | 验收 |
|---|---|---|
| **M0 骨架** | Go cmd + config + SQLite schema + `/health` + token 中间件 + install.sh/service install | 二进制起服务、鉴权通过、装成服务 |
| **M1 Claude 打通** | ClaudeProvider（子进程 stream-json）+ 事件账本 + `/send` + `/stream` | `curl` 新建会话/发消息/SSE 看到流式回复 |
| **M2 会话续接** | native_session_id 存取 + `--resume` + recent 投影 + 历史分页 | 重连上下文能接上；列表按更新时间排 |
| **M3 前端 core+Web** | core(TS) + React UI + Vite | 浏览器跑通：会话列表/会话流/发送/中断 |
| **M4 多机 + 桌面 App** | 机器清单切换 + Electron 壳（安全存储/深链/多 profile） | 桌面 App 管多台机器，端到端可用（**MVP 完成**） |
| **M5 打磨** | 断线续传/错误处理/workspace 白名单/Tailscale 接入文档/service 完善 | 杀进程重连无缝；一键部署 |
| **M6+ 扩展** | Android(Kotlin) → Codex provider → 服务端 render_state → 多渠道/团队/workflow/automation/审批 | 逐项接入，不改核心契约 |

---

## 14. 待确认（开工前）
1. 监听端口默认值（暂定 8787）。
2. claude 二进制发现方式：`PATH` 找 `claude`，还是配置里显式指定路径？（建议先 PATH + 可配置覆盖）
3. release 分发：GitHub Releases + install.sh（像 Garyx），还是先 `go install` 手动？
4. M0 是否现在就搭？（我可以先把 monorepo 目录 + Go 骨架 + SQLite + /health + token 中间件立起来）
```

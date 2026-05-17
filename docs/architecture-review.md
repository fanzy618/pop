# 架构评审：模块划分与边界

**状态**：草案
**日期**：2026-05-17
**目标**：以"高内聚、低耦合"为标尺，复盘当前模块切分与对外接口，找出阴影地带并给出可执行的改造路径。

---

## 1. 现状

### 1.1 模块依赖图

```
                ┌────────────────────────── cmd/pop ──────────────────────────┐
                │                                                             │
                ▼                                                             ▼
            console ──────────────────────────────────────────────► proxy ────► rules
                │                                                     │
                │     ┌──────────┬────────┬───────────┐               │
                ▼     ▼          ▼        ▼           ▼               ▼
            store ── config ── rules ── upstream ── telemetry      upstream
                            (depends on rules + upstream)              ▲
                                                                       │
                                                              telemetry (leaf)
buildinfo (leaf, only used by console)
```

依赖图无环。分层大致是：

| 层 | 包 | 角色 |
|---|---|---|
| L0 | `rules`、`upstream`、`telemetry`、`buildinfo` | 纯领域 / 基础设施叶子包 |
| L1 | `config` | 运行参数 + 跨包 DTO + 装配辅助 |
| L2 | `store` | SQLite 持久化 |
| L3 | `proxy` | 数据面 |
| L4 | `console` | 控制面（API + 静态资源） |
| L5 | `cmd/pop` | 装配 |

### 1.2 模块体量（LOC）

| 模块 | 主文件 LOC | 备注 |
|---|---|---|
| `console/server.go` | **859** | 单文件巨型 |
| `store/sqlite.go` | 704 | DDL + CRUD + 备份 |
| `proxy/server.go` | 579 | HTTP / CONNECT 数据面 |
| `telemetry/store.go` + `sysstats.go` | 214 + 183 | 活动 + 系统采样 |
| `config/config.go` | 164 | 三种职能混杂 |
| `rules/rules.go` | 141 | 纯函数式 |
| `upstream/manager.go` | 102 | Transport 池 |

### 1.3 对外接口快照

- **`rules.Matcher`**：`NewMatcher / Decide / GeneratePAC`。无依赖、可单测、内聚。
- **`upstream.Manager`**：`NewManager / Replace / Get / All`。snapshot-on-replace，线程安全。内聚。
- **`telemetry.Store`**：`Start / Finish / Snapshot / Recent / Subscribe / StartJanitor`。同时是计数器、环形缓冲、事件总线、TTL 回收器——单一接口承担四种角色，但目前规模可控。
- **`config.Config`**：`Default / Validate / BuildMatcher / BuildUpstreamConfigs` + 三个 DTO（`Config`、`RuleConfig`、`UpstreamConfig`）+ `ValidateRuntime` 自由函数。
- **`store.SQLite`**：直接暴露 `CreateRule / ListRulesPage / Backup / Restore / …`，返回 `config.RuleConfig`。
- **`proxy.Server`**：`NewServer / NewServerWithMatcher / NewServerWithDeps` + `SetMatcher / GetMatcher / SetUpstreams / GetUpstreams / SetTelemetry`。
- **`console.Server`**：`NewServer(cfg, db, proxy, telStore, sysStats)`——全员依赖。

---

## 2. 问题清单

### P1 — `config` 是个"什锅炖" *(高 / 影响 store、console、rules)*

[`internal/config/config.go`](internal/config/config.go) 同时承担：

1. 运行参数（`ProxyListen / ConsoleListen / SQLitePath / DefaultAction`）；
2. 跨模块持久化 DTO（`RuleConfig / UpstreamConfig`）；
3. 装配函数（`BuildMatcher` 是 `*Config` 的方法，但只读 `DefaultAction`；`BuildUpstreamConfigs` 是包级函数）；
4. 业务校验（`ValidateRuntime`）。

**后果**：

- `store` 仅仅为了用 `RuleConfig` 类型，被迫 `import config`。本应"持久化"独立于"配置"。
- `BuildMatcher` 挂在 `*Config` 是误导——它把 rules 装入 matcher，并不依赖 Config 的字段（仅用了 `DefaultAction`）。
- `console` 同时引用 `config.Config`、`config.RuleConfig`、`config.UpstreamConfig`，分不清"PUT /api/config 收到的是哪一类"。
- `ValidateRuntime` 在循环里对值副本做 `rule.BlockStatus = 404` 的赋值无效 ([config.go:105](internal/config/config.go:105))，是一个真正的 dead-code 隐患。

### P2 — `console` 是上帝包 *(高)*

`console/server.go` 859 行单文件涵盖：HTTP 路由、页面渲染、Config/Upstream/Rule CRUD、Backup/Restore、ABP 解析、PAC 生成、SSE、Stats history、运行时重建编排、route-target 解析。它依赖 `buildinfo / config / proxy / rules / store / telemetry / upstream` **全部** 7 个内部包，是依赖图的下水道。

**后果**：

- 任何上游改动几乎都会触达本文件。
- ABP 解析（[server.go:766](internal/console/server.go:766)）、route-target 解析（[server.go:740](internal/console/server.go:740)）、`isForeignKeyConstraint` 字符串嗅探（[server.go:732](internal/console/server.go:732)）等通用逻辑寄生在 HTTP 层，不便复用、不便测。
- 单元测试只能通过 `httptest` 间接覆盖。

### P3 — `proxy.Server` 通过多个 setter 接收外部状态 *(中)*

[`proxy/server.go:90-149`](internal/proxy/server.go:90)：`SetMatcher / SetUpstreams / SetTelemetry / GetMatcher / GetUpstreams` 共 5 个独立 RWMutex 方法。每次 `console` CRUD（[server.go:704-706](internal/console/server.go:704)）连续触发三个 setter。

**后果**：

- 不同步：matcher 已经换、upstream 还没换的瞬间存在；当前匹配后即刻 dispatch，但语义脆弱。
- 数据面每请求至少做 2 次 `RLock`（[server.go:107](internal/proxy/server.go:107)、[server.go:151](internal/proxy/server.go:151)）。
- 依赖方向反转——proxy 是"被推数据"的对象，可以更显式地建模成"路由快照订阅者"。

### P4 — `proxy` 构造函数三选一 *(低)*

`NewServer / NewServerWithMatcher / NewServerWithDeps` 同时存在；`NewServerWithDeps` 内部还会构造一份默认 `telemetry.Store`（[server.go:86](internal/proxy/server.go:86)），随后被 `main.go` 用 `SetTelemetry` 覆盖——构造期的实例直接被 GC 抛弃。三个构造函数仅服务于不同测试入口，模糊了"什么是必须依赖"。

### P5 — `handleConfig` GET / PUT 不对称 *(低)*

GET 返回手写的 `map[string]any{"proxy_listen": …, "upstreams": …, "rules": …}`（[server.go:199](internal/console/server.go:199)），PUT 接收 `config.Config`（不含 upstreams/rules，会被静默丢弃）。前端契约脆弱，无类型化响应。

### P6 — `telemetry` 同时承担四种角色 *(中)*

`telemetry.Store` 是聚合计数器（`Snapshot`）、环形事件缓冲（`Recent`）、事件总线（`Subscribe`）和 TTL 回收器（`StartJanitor`）的合体。`Event` 结构体直接带 JSON tag，被 SSE 层直接序列化（[server.go:649](internal/console/server.go:649) 周围）。

**后果**：

- 想替换为 Prometheus 风格指标，或者把 SSE 抽换成 WebSocket，都需要改 `telemetry` 类型。
- `SysStatsCollector` 通过 `statsFn func() Stats` 反查 `Store`，是一种隐式的"反向依赖"——单元测试要传一个返回 Stats 的闭包，可读性差。

### P7 — Rule ID 类型在边界处转换 *(低)*

`config.RuleConfig.ID` 是 `int64`，但 `rules.Rule.ID` 是 `string`；`BuildMatcher` 在两者间字符串化（[config.go:142](internal/config/config.go:142)）。Telemetry 的 `Event.RuleID` 又回到 `string`。整条链路上 ID 类型来回摇摆，没人受益。

### P8 — `cmd/pop` 缺少优雅停机 *(低)*

[main.go:69-80](cmd/pop/main.go:69) 启动 console、proxy 两个 `http.Server`，没有 `signal.Notify`、没有 `srv.Shutdown`、`sysStats.Start(context.Background())` 永不取消。重启时 SSE 连接、in-flight 请求被硬切。

### P9 — 全部依赖均为具体类型，无 interface seam *(中)*

`console.NewServer` 签名（[server.go:54](internal/console/server.go:54)）：

```go
func NewServer(cfg *config.Config, db *store.SQLite, proxyServer *proxy.Server,
               telemetryStore *telemetry.Store, sysStats *telemetry.SysStatsCollector)
```

全部是包内具体类型。后果是测试时无法注入假实现（必须真起 SQLite），并且未来如果想插入"内存存储"或"远程 telemetry sink"，必须改类型签名而不是换实现。

### P10 — 每次 CRUD 全量重建运行时 *(低，前置规模约束)*

每个 rule/upstream 修改都触发 `db.ListRules + db.ListUpstreams + upstream.NewManager(全量构造 Transport 池) + BuildMatcher`（[server.go:686-707](internal/console/server.go:686)）。当前规则量 ~2.5k，可接受；增量更新还没有出现的迫切性，列在这里仅供未来参考。

---

## 3. 改进方案

总体节奏：**每步一个 PR、向后兼容、不动数据面行为**。

### 改进 1 — 拆 `config`，把 DTO 解放出来 *(对应 P1、P7)*

新增 `internal/model`（或合并入 `internal/store`）承担持久化 DTO：

```
internal/model/
    rule.go        // type Rule struct{ ID int64; Pattern string; ... }
    upstream.go    // type Upstream struct{ ... }
```

- `config` 只剩 `Config{ProxyListen, ConsoleListen, SQLitePath, DefaultAction, PACProxyAddr}` 和 `Validate`。
- `BuildMatcher` 移到 `rules`（或新 `internal/routing`）：`rules.MatcherFromModels(items []model.Rule, defaultAction Action) *Matcher`。
- `BuildUpstreamConfigs` 移到 `upstream` 包，签名直接吃 `[]model.Upstream`。
- `store` 不再 `import config`；`console` 也不再混用 `config.RuleConfig` 与 `config.Config`。
- 顺手把 `ID` 类型在 `rules` 内统一为 `int64`，消除 P7 的字符串往返。

**收益**：彻底打破 `store → config` 的奇怪依赖；`config` 变成真正"运行参数包"，做到单一职责。

### 改进 2 — `proxy` 用快照取代 setter *(对应 P3、P4)*

引入：

```go
// internal/proxy/snapshot.go
type RouteSnapshot struct {
    Matcher       *rules.Matcher
    Upstreams     *upstream.Manager
    DefaultAction rules.Action
}

type Server struct {
    snapshot atomic.Pointer[RouteSnapshot]
    telemetry *telemetry.Store
    // ...
}

func (s *Server) Publish(snap *RouteSnapshot) { s.snapshot.Store(snap) }
```

- 删除 `SetMatcher / SetUpstreams / GetMatcher / GetUpstreams`；保留单一 `Publish`。
- 数据面 `decide()` 改成 `snap := s.snapshot.Load()`，无锁。
- 构造函数收敛成一个：`NewServer(initial *RouteSnapshot, tel *telemetry.Store, opts ...Option)`，删除 `NewServer` 与 `NewServerWithMatcher`。
- 顺带把 P4 里"内部构造冗余 telemetry.Store 然后被覆盖"的浪费一并消除。

**收益**：原子切换、无锁热路径、构造期依赖一目了然。

### 改进 3 — 拆 `console` 成若干小包 *(对应 P2、P5)*

按现有功能边界切分（每个文件 100–200 LOC 起步）：

```
internal/console/
    server.go        // mux 装配 + 中间件，<150 LOC
    routes/
        config.go    // /api/config GET/PUT （定义类型化的 ConfigResponse）
        rules.go
        upstreams.go
        data.go      // backup/restore/import-abp
        stats.go     // stats + history + activities + SSE
        pac.go
    reloader.go      // 唯一的 RuntimePublisher.Publish 调用方
internal/abp/
    parser.go        // 现在埋在 console 里的 parseABPDomains 等
internal/routing/
    target.go        // parseImportRouteTarget
```

- ABP 解析、route-target 解析、PAC 字符串生成是纯函数，搬出去后独立可测。
- `handleConfig` GET/PUT 改用类型化的 `ConfigDTO`，PUT 显式忽略 read-only 字段。
- 静态资源仍然 `//go:embed`，但只在 `server.go` 里挂载。

**收益**：单文件 859 → ~150 LOC；console 不再是"上游万物的下游"，可读性、可测性显著提升。

### 改进 4 — 在 `console ↔ store / proxy` 边界引入接口 *(对应 P9)*

`console` 里定义它真正需要的窄接口：

```go
type RuleRepo interface {
    ListRules() ([]model.Rule, error)
    ListRulesPage(opts RuleListOptions) (RuleListPage, error)
    CreateRule(*model.Rule) error
    UpdateRule(id int64, r model.Rule) error
    DeleteRule(id int64) error
}

type UpstreamRepo interface { /* … */ }
type Backuper     interface { Backup() (BackupPayload, error); Restore(BackupPayload) error }
type RuntimePublisher interface { Publish(snap *proxy.RouteSnapshot) error }
type ActivityFeed interface {
    Recent(limit int) []telemetry.Event
    Subscribe(buf int) (<-chan telemetry.Event, func())
}
```

`store.SQLite` 实现这些接口；`console.NewServer` 改为接收接口。

**收益**：把"具体实现的形状"和"console 真正需要的形状"分开；单测可以塞 in-memory 假实现；未来换持久层不动 console。
**注意**：不要为接口而接口——只把*确实存在第二种实现可能或确实需要在测试里替换*的边界做接口（store、telemetry 是值得的，rules.Matcher 不值得）。

### 改进 5 — 把 telemetry 的四种角色显式分开 *(对应 P6)*

`telemetry` 内部按角色拆类型，对外保持兼容：

```
telemetry.Counters       // Snapshot()
telemetry.EventBuffer    // Recent / cleanupExpired
telemetry.EventBus       // Subscribe / publish
telemetry.SysSampler     // History
```

`Store` 退化为这四者的薄组合层。Event 上的 JSON tag 移到 `console/routes/stats.go` 里独立的 DTO，让 telemetry 的内部类型独立于 wire format。

**收益**：未来要换计数后端（Prometheus）、SSE 改 WebSocket、活动落盘等改动都只动一层。

### 改进 6 — `cmd/pop` 接入信号 & 优雅停机 *(对应 P8)*

```go
ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
defer cancel()
sysStats.Start(ctx)
go func() { <-ctx.Done(); consoleSrv.Shutdown(...); srv.Shutdown(...) }()
```

加 30s 超时；trigger 时一并 `telStore.CleanupExpired` flush 给订阅者。

---

## 4. 执行节奏建议

| 顺序 | 改进 | 风险 | 收益排序 |
|---|---|---|---|
| 1 | 改进 1（拆 `config`） | 低 — 类型搬迁，编译器盯着 | 解锁后续，必做 |
| 2 | 改进 2（`proxy` 快照） | 中 — 数据面行为改动 | 高 |
| 3 | 改进 3（`console` 拆分） | 中 — 文件多但小步走 | 高 |
| 4 | 改进 4（接口 seam） | 低 — 改 import | 中 |
| 5 | 改进 6（优雅停机） | 低 | 中 |
| 6 | 改进 5（telemetry 拆角色） | 中 — 触及 SSE wire format | 低优先级，可观望 |

每步前后都跑 `go test ./...`；改进 2、3 需要补 unit 测试（特别是 `RouteSnapshot` 切换、ABP 解析独立测试）。

## 5. 不建议做的事

- 把 `rules` 再拆为子包：当前 141 LOC 已经高内聚，拆只增加 import 噪声。
- 引入 ORM、把 SQLite 换 BadgerDB：与"低资源、个人使用"目标不符。
- 为所有类型加接口：仅在改进 4 列出的边界值得；其余保持具体类型，避免"接口污染"。
- 上 DI 容器 / wire 类工具：装配在 `cmd/pop/main.go` 50 行内可以读完，手工组装就够。

# 测试方案与用例设计

**状态**：草案
**日期**：2026-05-17
**适用范围**：基于当前主分支的功能面（代理数据面 + Console API + SQLite 持久化 + 遥测）。

> 本文与既有的 [docs/testing-strategy.md](docs/testing-strategy.md) 不冲突：后者写"分层原则"，本文写"按现有功能逐项落用例 + 暴露的覆盖缺口"。

---

## 1. 现状盘点

### 1.1 测试矩阵规模

| 层级 | 数量 | 位置 |
|---|---:|---|
| 单元测试 | **33** | `internal/**/*_test.go`、`cmd/pop/main_test.go` |
| 集成测试 | **27** | `integration/m1_*.go … m7_*.go`（按里程碑组织） |

### 1.2 各模块覆盖情况

| 模块 | 覆盖较好 | 偏薄 / 未覆盖 |
|---|---|---|
| `rules` | 最长前缀命中、默认动作、各种 pattern | 大规模 rules 下的 benchmark；`GeneratePAC` 内嵌字符串边界 |
| `upstream` | Replace、scheme 拒绝 | 替换时旧 `Transport.CloseIdleConnections` 行为、in-flight 请求语义 |
| `telemetry/store` | 容量上限、TTL 清理、Subscribe | 慢消费者背压（select-default 丢弃）、并发 Start/Finish race |
| `telemetry/sysstats` | CPU/Mem 计算、capacity、window | Linux `/proc/stat` 解析正确性（无 fixture）、长跑场景 |
| `config` | 默认值、`http://` 限制 | `Validate` 边界、`BuildMatcher` 排序稳定性、`ValidateRuntime` 失败分支 |
| `store/sqlite` | CRUD、外键、备份回放、同 pattern 覆盖 | 备份中途失败的原子性、并发写、UpdateRule 外键 |
| `proxy` | DIRECT/CONNECT/BLOCK/upstream 分流、loop 检测、合规头、host 解析 | 并发热切换、上游 CONNECT 非 200、超时、慢响应、`responseRecorder` 字节统计准确性 |
| `console` | CRUD、分页搜索、SSE、备份回放、ABP 导入、PAC | `PUT /api/config` 全链路、`/api/stats/history` 形状、ABP 边界（`@@`、`##`、`/regex/`、重复、空行）、并发 CRUD 触发的 `rebuildRuntime` 竞态 |
| `cmd/pop` | 配置解析优先级 | 启动后端口冲突、SIGINT 停机（功能本身缺失） |

### 1.3 Console handler 对应关系

| 路由 | 是否有专属测试 |
|---|---|
| `/api/version` | ✅ `TestConsoleVersion` |
| `/api/config` GET | ⚠️ 被 `TestConsoleNoAuthRequired` 顺带探了一下 |
| `/api/config` PUT | ❌ **未覆盖** |
| `/api/upstreams` (GET/POST/PUT/DELETE) | ✅ CRUD + Name optional |
| `/api/rules` (GET/POST/PUT/DELETE) | ✅ CRUD + 分页 + 搜索 + 重写覆盖 |
| `/api/rules/reorder` | ⚠️ 只验证返回 `reorder_disabled` |
| `/api/data/backup` `/restore` `/import-abp` | ✅ 路径覆盖，边界单薄 |
| `/api/stats` | ✅ |
| `/api/stats/history` | ❌ **未覆盖** |
| `/api/activities` `/api/activities/stream` | ✅ |
| `/proxy.pac` | ✅ |

---

## 2. 金字塔与速度目标

| 层级 | 数量预期 | 单测耗时目标 | 套件总耗时目标 |
|---|---|---|---|
| 单元 | ~60 | < 10 ms | < 1 s |
| 集成（同进程 `httptest.Server`） | ~35 | < 200 ms | < 5 s |
| 端到端（真起进程 + 真起 SQLite + curl） | ≤ 5 | 数秒 | < 15 s |
| Benchmark / load | 手动触发 | n/a | 不进 CI |

**原则**：

1. 业务关键路径必有集成测试；纯逻辑必有单元测试。
2. 凡是"动态切换 / 热重载"的地方必须有并发测试。
3. 凡是"持久化"的地方必须有失败回滚测试。
4. 不为 trivial getter/setter、不为框架代码、不为脚本写测试。
5. 测试必须可重入（不依赖外网、不依赖固定端口）。

---

## 3. 按模块的用例设计

### 3.1 `internal/rules` — 决策核心

**已有**：`TestMatcherLongestPatternWins / TestMatcherPatterns / TestMatcherDefaultDecision`。

**新增建议**：

| 类型 | 用例 | 说明 |
|---|---|---|
| 单元 | `TestMatcher_TieBreakByCreatedAt` | 同长度时"较新优先"被 BuildMatcher 排序，验证组合行为 |
| 单元 | `TestMatcher_DisabledIgnored` | 验证 `Enabled=false` 永不参与决策 |
| 单元 | `TestMatcher_PatternEdgeCases` | 单 label（`svc`）、含点尾（`foo.`）、大小写、Unicode/IDN |
| 单元 | `TestMatcher_BlockDefaultStatus` | BLOCK 决策若 `BlockStatus==0` 自动填 404 |
| 单元 | `TestGeneratePAC_AllActions` | DIRECT/PROXY/BLOCK 三种动作生成的 PAC 行内容 |
| 单元 | `TestGeneratePAC_EscapesPattern` | pattern 含 `"` `\` 时 `%q` 转义不破坏 PAC JS |
| Benchmark | `BenchmarkMatcher_Decide_4kRules` | 模拟生产规模，4000 条规则做 100 次 Decide |

### 3.2 `internal/upstream`

| 类型 | 用例 |
|---|---|
| 单元 | `TestManager_Replace_ClosesRemovedTransport`：删除一个 upstream 后旧 Transport 的 `CloseIdleConnections` 被调用 |
| 单元 | `TestManager_Replace_KeepsUnchangedTransport`：相同 ID + URL 保留旧 Transport（若实现允许；当前实现总是重建——若不打算优化，则反向断言"每次 Replace 都新建"以锁定行为） |
| 单元 | `TestManager_DisabledNotIncluded`：`Enabled=false` 不会出现在 `All()` |
| 集成 | `TestUpstreamReplace_InFlightSurvives`：起一个慢 upstream，发起 request 中途 Replace 删除该 upstream，期望已发出的请求能完成（或显式断言被切断） |

### 3.3 `internal/telemetry`

**已有**：容量、TTL、Stats、Subscribe。

**新增建议**：

| 类型 | 用例 |
|---|---|
| 单元 | `TestStore_SubscribeSlowConsumer_DropsWithoutBlock`：buffer 满后 Finish 不阻塞，丢事件而非死锁 |
| 单元 | `TestStore_Subscribe_UnsubscribeCloses`：unsubscribe 后通道关闭，二次 unsubscribe 安全 |
| 单元 | `TestStore_ConcurrentStartFinish_RaceClean`：1000 goroutines × Start/Finish，`-race` 通过；最终 InFlight==0 |
| 单元 | `TestStore_CapacityWithTTL_BothEnforced`：先打满 capacity，再让 TTL 过期，断言两条路径都参与裁剪 |
| 单元 | `TestSysStats_SamplesGoroutineCount`：触发 goroutine 增长，下一次 sample 体现 |
| Fixture | `TestSysStats_ParseProcStat_Linux`：用 fixture 字符串验证 `/proc/stat` 解析（建议把读文件与解析分离） |

### 3.4 `internal/config`

| 类型 | 用例 |
|---|---|
| 单元 | `TestValidate_AllRequiredFields`：分别置空 ProxyListen、ConsoleListen、不合法 DefaultAction，期望各自报错 |
| 单元 | `TestBuildMatcher_StableOrderByCreatedAtThenID`：构造同时间多条规则，断言 ID 倒序稳定 |
| 单元 | `TestValidateRuntime_RulesReferenceUnknownUpstream`：PROXY 规则指向不存在 upstream → 报错 |
| 单元 | `TestValidateRuntime_BlockStatusBounds`：BlockStatus = -1 / 600 → 报错 |
| 单元（缺陷探测） | `TestValidateRuntime_BlockStatusZeroBehavior`：当前实现对值副本赋值无副作用（见 [config.go:105](internal/config/config.go:105)），如果将来想保留 `0→404` 兜底，需要先暴露这个测试再修 |

### 3.5 `internal/store`

| 类型 | 用例 |
|---|---|
| 单元 | `TestRestore_AtomicOnPartialFailure`：构造一份 upstreams ok 但 rules 引用了缺失 upstream 的 backup，期望 restore 失败且原数据完整 |
| 单元 | `TestUpdateRule_UnknownUpstreamForeignKey`：UpdateRule 的外键错误路径（当前仅 CreateRule 路径有等价测试） |
| 单元 | `TestRulesPagination_TotalAndOffset`：覆盖 page>pageCount 的回退 |
| 单元 | `TestListRulesPage_KeywordCaseInsensitiveLike`：验证 keyword 大小写、特殊字符（`%`、`_`）转义 |
| 单元 | `TestSchemaIdempotent_RepeatedOpen`：两次 OpenSQLite 同一个文件，不报"已存在"错 |

### 3.6 `internal/proxy`

**已有**：DIRECT、CONNECT、BLOCK、upstream 分流、loop 检测、合规头。

**新增建议**：

| 类型 | 用例 |
|---|---|
| 集成 | `TestProxy_ConnectViaUpstream_NonHTTPResponse`：上游 CONNECT 返回 502，期望客户端拿到 502 不挂起 |
| 集成 | `TestProxy_HotReloadDuringTraffic`：并发 100 个 request 同时多次 `Publish`/`SetMatcher`，期望无 panic、无 race、最终决策一致 |
| 集成 | `TestProxy_SlowUpstream_ResponseHeaderTimeout`：上游延迟 30s 返回 header，断言 20s 后客户端拿到 502（验证 transport 上的 `ResponseHeaderTimeout`） |
| 集成 | `TestProxy_LargeBody_BytesAccounted`：上传/下载 10MB，断言 `telemetry.Snapshot()` 的 `BytesIn/BytesOut` 与实际字节数一致（验证 `responseRecorder`） |
| 集成 | `TestProxy_HostHeaderEdgeCases`：缺失端口、IPv6 (`[::1]:80`)、尾点 `example.com.`、大小写混合 |
| 单元 | `TestNormalizeHost_IPv6` |

### 3.7 `internal/console`

| 类型 | 用例 |
|---|---|
| 集成 | `TestConfig_PUT_ChangesDefaultAction`：PUT 改 default_action → 后续未命中规则的请求按新动作处理 |
| 集成 | `TestConfig_PUT_PACOverride`：PUT 改 `pac_proxy_addr` → `/proxy.pac` 输出对应地址 |
| 集成 | `TestStatsHistory_ReturnsSamplesAndShape`：起 SysStatsCollector 用 100ms 间隔，调一次 history 断言 JSON 字段齐全、按时间升序 |
| 集成 | `TestImportABP_SkipsRegexAndException`：构造含 `/regex/`、`@@`、`##`、空行、注释的 ABP 文本，断言只入库正常域名 |
| 集成 | `TestImportABP_DedupesWithinFile`：同 host 出现 5 次只入库一次 |
| 集成 | `TestImportABP_RouteTargetUpstream`：route_target=`UPSTREAM:1` 入库时 action=PROXY、upstream_id=1 |
| 集成 | `TestRules_ConcurrentCRUD_NoRebuildRace`：N goroutines 并发 POST/DELETE/GET rules，`-race` 干净，最终 matcher 状态与 DB 一致 |
| 集成 | `TestSSE_ClientDisconnect_Unsubscribes`：建立 SSE 连接后客户端断开，订阅被回收（断言 `Store.subs` 计数恢复——可经由再 Subscribe 后总数验证） |
| 集成 | `TestBackup_RoundtripPreservesCreatedAt`：备份→清空→恢复后 `created_at` 与 ID 不变 |
| 集成 | `TestPAC_ProxyAddrInferenceWithIPv6`：请求 PAC 的 Host 是 IPv6 时端口拼接正确 |

### 3.8 `cmd/pop` 与端到端

| 类型 | 用例 |
|---|---|
| E2E | `TestE2E_StartStop_Direct`：脚本启动二进制 → curl 一个 direct 请求 → 验证 200 → SIGINT 关闭、退出码 0 |
| E2E | `TestE2E_ConfigPersistsAcrossRestart`：与现有 `m4_persistence_test.go` 互补，跑真二进制而非同进程 |
| 单元 | `TestResolveRuntimeConfig_FlagParseError`：传非法 flag，期望明确错误信息 |

---

## 4. 覆盖缺口（按优先级）

| 优先级 | 缺口 | 风险 | 建议用例 |
|---|---|---|---|
| **P0** | 热重载并发安全 | 数据面崩溃 / race | §3.6 `TestProxy_HotReloadDuringTraffic`、§3.7 `TestRules_ConcurrentCRUD_NoRebuildRace` |
| **P0** | `PUT /api/config` 完全未测 | 改 default_action / PAC override 后行为不可知 | §3.7 两条 PUT 测试 |
| **P1** | `/api/stats/history` 未测 | UI 图表的唯一数据源 | §3.7 `TestStatsHistory_*` |
| **P1** | Restore 原子性 | 失败后数据库可能半残 | §3.5 `TestRestore_AtomicOnPartialFailure` |
| **P1** | 上游 CONNECT 失败 / 慢响应 | 客户端挂起或泄漏 fd | §3.6 两条 |
| **P1** | SSE 慢消费者 | 写阻塞 → 影响代理路径（同 Store mutex） | §3.3 `TestStore_SubscribeSlowConsumer_*` |
| **P2** | ABP 边界 | 误入库 `@@example.com` 这种异常项 | §3.7 `TestImportABP_*` |
| **P2** | Linux `/proc/stat` 解析 | 跨内核版本格式变化 | §3.3 `TestSysStats_ParseProcStat_Linux`（需先把 IO 与解析拆开） |
| **P2** | Bytes 统计准确性 | telemetry 数字漂移 | §3.6 `TestProxy_LargeBody_BytesAccounted` |
| **P3** | Benchmark | 规则规模回归 | §3.1 `BenchmarkMatcher_Decide_4kRules` |
| **P3** | 真二进制 E2E | 集成测试漏掉的装配错误 | §3.8 两条 |

---

## 5. 工程化

### 5.1 CI 必跑

```bash
go vet ./...
go test -race -count=1 ./...        # 必加 -race
```

当前 `make lint` 没有带 `-race`；建议加。

### 5.2 覆盖率门槛（指导，不强行卡 CI）

| 包 | 目标行覆盖率 | 原因 |
|---|---|---|
| `internal/rules` | ≥ 90% | 决策核心 |
| `internal/config` `internal/upstream` `internal/telemetry` | ≥ 80% | 基础设施 |
| `internal/store` `internal/proxy` | ≥ 75% | 含 IO 与超时分支，不强求 |
| `internal/console` | ≥ 70% | UI 资产模板部分不必苛求 |

执行：

```bash
go test -coverprofile=/tmp/cov.out ./...
go tool cover -func=/tmp/cov.out | tail -20
```

### 5.3 Benchmark / Load（手动触发，不进 CI）

- `go test -bench=. -benchmem ./internal/rules`：matcher 在 4k 规则下应稳定在 µs 级。
- 简单 `wrk -t4 -c100 -d30s http://127.0.0.1:5128` + 一个上游 mock：观察 inFlight、`/api/stats/history` 中 goroutine/heap 不爆。

### 5.4 测试组织建议

- 在每个 `m*_test.go` 顶部加 `//go:build integration` build tag 后续可拆 `make test-unit` / `make test-integration`，CI 矩阵化（目前所有测试一起跑，单体 < 5s 还可以接受，不急）。
- 共享的 testing helpers（开 SQLite、起 mock upstream、起 `httptest.NewServer(consoleHandler)`）抽到 `integration/testutil_test.go`，目前每个集成文件多少都在重复造。

### 5.5 测试可重入与依赖

| 资源 | 现状 | 建议 |
|---|---|---|
| 端口 | 集成测试已用 `:0` | 保持 |
| SQLite | 用 `t.TempDir()` | 保持 |
| 时钟 | 通过 `time.Now()` 直接读 | 引入 `nowFn` 注入点供 `telemetry` 与 `store` 使用（目前测试只能 `time.Sleep`，慢） |
| 外网 | 无 | 保持，绝不引入 |

---

## 6. 不做的事

- **UI 自动化**（Playwright/Selenium）：项目阶段不需要，且与 [AGENTS.md](AGENTS.md) 一致。
- **Mutation testing / Fuzzing-everything**：单点价值低；只在 ABP parser、`parseImportRouteTarget` 等纯文本解析处加 Go native fuzz（`func FuzzParseABPLineHost`）。
- **mock 数据库**：modernc.org/sqlite 是纯 Go，`t.TempDir()` 起一个真实库已经足够快。
- **覆盖率刷分**：不为了到 90% 去测 getter。覆盖目标只用来指示薄弱区域。

---

## 7. 第一周可落地清单

按"少量改动 + 高价值"排序，可立即开干：

1. CI 加 `-race`。
2. `internal/console` 补 `TestConfig_PUT_*`、`TestStatsHistory_*`、`TestImportABP_*` 三组。
3. `internal/proxy` 补 `TestProxy_HotReloadDuringTraffic`、`TestProxy_LargeBody_BytesAccounted`。
4. `internal/store` 补 `TestRestore_AtomicOnPartialFailure`。
5. 抽出 `integration/testutil_test.go` 公共工具，减少集成测试重复。

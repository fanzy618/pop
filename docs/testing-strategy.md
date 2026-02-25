# 测试策略

## 1. 目标

- 确保核心路由行为稳定可回归。
- 保证配置演进与接口迭代不破坏既有能力。
- 用本地可重复测试替代依赖外部网络的不确定性。

## 2. 分层策略

### 2.1 单元测试

覆盖模块内部逻辑：

- `internal/rules`：匹配语义与顺序逻辑。
- `internal/config`：配置校验、读写、原子保存。
- `internal/upstream`：上游配置合法性与管理逻辑。
- `internal/telemetry`：统计累加、容量边界、TTL 清理。
- `internal/proxy`：host 解析归一化等基础逻辑。

### 2.2 集成测试

覆盖跨模块行为：

- 代理 DIRECT 与 CONNECT 路径。
- BLOCK 路径。
- A/B 上游分流。
- 重启后配置恢复。
- Console API 鉴权、CRUD、SSE。

## 3. 当前测试矩阵

- M1：`integration/m1_direct_test.go`、`integration/m1_connect_test.go`
- M2：`integration/m2_block_test.go`
- M3：`integration/m3_upstream_test.go`
- M4：`integration/m4_persistence_test.go`
- M5：`integration/m5_telemetry_test.go`
- M6：`integration/m6_console_api_test.go`

## 4. 回归策略

- 规则、路由、代理核心改动必须跑全量：`go test ./...`。
- 配置模型字段变更必须补对应持久化与校验测试。
- Telemetry 结构变更必须验证有界内存行为。

## 5. 非覆盖范围说明

- 当前不包含 Console UI 自动化测试（符合项目阶段约束）。
- 当前不包含真实公网依赖测试，避免 flaky。

## 6. 建议扩展

- 增加基准测试（规则数量增长场景）。
- 增加故障注入测试（上游抖动、超时、断连）。
- 增加并发压测脚本，验证长时间运行稳定性。

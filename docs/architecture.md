# 架构设计

## 1. 总体架构

POP 采用单进程、模块化结构：

- 代理面（Data Plane）：处理用户代理流量。
- 控制面（Control Plane）：管理配置、规则、上游、观测接口。

### 核心模块

- `cmd/pop/main.go`：进程入口与服务装配。
- `internal/config`：配置模型与校验。
- `internal/rules`：规则匹配器与决策对象。
- `internal/proxy`：HTTP/CONNECT 代理执行、DIRECT/PROXY/BLOCK。
- `internal/upstream`：上游代理管理与 transport 复用。
- `internal/telemetry`：活动流与统计聚合。
- `internal/console`：Console API。

## 2. 请求处理流程

1. 客户端将请求发送到 POP 代理端口。
2. POP 解析目标 host（HTTP URL 或 CONNECT Host）。
3. host 归一化（小写、端口处理、尾点处理）。
4. 规则引擎按顺序匹配，得到决策动作。
5. 执行动作：
   - `DIRECT`：直接访问目标。
   - `PROXY`：选择对应上游 transport 转发。
   - `BLOCK`：立即返回配置状态码。
6. 记录活动事件并更新统计计数。

## 3. 配置与热生效

- 配置源：默认值 + 环境变量 + 命令行参数（`CLI > ENV > default`）。
- 启动时执行结构与业务校验。
- Console API 更新配置后：
  1. 校验新配置。
  2. 构建新 matcher 与 upstream manager。
  3. 应用到 proxy 运行态。

该流程保证运行态更新一致性，并尽量降低中断风险。

## 4. 资源控制策略

- 连接复用：`http.Transport` 复用、限制空闲连接。
- 超时控制：dial、TLS、response header 等超时均设置默认值。
- 活动日志：有界内存（capacity）+ TTL 过期清理。
- 统计数据：仅维护聚合值（计数与字节），不保存无限增长历史。

## 5. 可观测性设计

- 活动事件：记录请求来源、目标、动作、状态、耗时、字节数等。
- 运行统计：并发数、总请求、错误数、入/出字节。
- 实时流：通过 SSE 将活动事件推送给 Console。

## 6. 错误处理原则

- 配置错误：拒绝加载/应用并返回明确错误。
- 上游不可用：返回 `502 Bad Gateway`。
- BLOCK 动作：按规则阻断码返回，Web Console 创建规则时固定为 `404`。

## 7. 演进方向

- 可在现有模块基础上扩展 SOCKS5、规则分组、更强认证方式。
- 保持“数据面稳定、控制面可演进”的架构原则，减少回归风险。

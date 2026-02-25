# 运行与维护指南

## 1. 启动方式

```bash
go run ./cmd/pop -config ./pop.json
```

默认会启动：

- 代理服务：`proxy_listen`
- Console API：`console_listen`

## 2. 配置管理

- 配置文件为 JSON（示例见根目录 `README.md`）。
- 配置更新由 Console API 触发并原子落盘。
- 关键配置项：
  - `proxy_listen`
  - `console_listen`
  - `auth.username` / `auth.password`
  - `default_action`
- 运行期规则与上游保存在 SQLite（通过 Console API 管理）

## 3. 运行期观测

### 3.1 统计

- `GET /api/stats`
- 重点关注：
  - `in_flight`
  - `total_requests`
  - `total_errors`
  - `bytes_in`
  - `bytes_out`

### 3.2 活动

- `GET /api/activities?limit=N`
- `GET /api/activities/stream`（SSE）

## 4. 资源控制建议

- 保持 telemetry 的容量上限和 TTL，避免活动数据无界增长。
- 个人使用下默认 transport 参数通常足够，不建议盲目放大连接上限。
- 若长时间运行，建议定期观察错误率与并发峰值。

## 5. 常见问题排查

### 5.1 请求未按预期走上游

- 检查规则顺序，是否被更前规则命中。
- 检查规则的 `upstream_id` 是否存在且上游已启用。
- 检查上游 URL 是否 `http://` 且可达。

### 5.2 Console 返回 401

- 检查 Basic Auth 用户名和密码。
- 确认请求确实发到 `console_listen`。

### 5.3 BLOCK 状态码不符合预期

- Web Console 下 `BLOCK` 状态码固定为 `404`。
- 检查是否命中到了其他更前规则。

### 5.4 修改配置后行为未更新

- 检查配置接口返回是否成功。
- 检查配置校验是否失败被拒绝。

## 6. 维护建议

- 每次改动后执行：

```bash
go test ./...
```

- 变更规则匹配逻辑时，务必新增对应单测和集成回归测试。

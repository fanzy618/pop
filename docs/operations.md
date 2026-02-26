# 运行与维护指南

## 1. 启动方式

默认启动：

```bash
go run ./cmd/pop
```

默认监听：

- 代理服务：`0.0.0.0:5128`
- Console API：`127.0.0.1:5080`

可通过环境变量或命令行覆盖，优先级：`CLI > ENV > 默认值`。

## 2. 配置覆盖

环境变量：

- `POP_PROXY_LISTEN`
- `POP_CONSOLE_LISTEN`
- `POP_DEFAULT_ACTION`
- `POP_SQLITE_PATH`

命令行（GNU 风格）：

- `--proxy-listen` / `-p`
- `--console-listen` / `-c`
- `--default-action` / `-a`
- `--sqlite-path` / `-s`

## 3. 运行期观测

- `GET /api/stats`
- `GET /api/activities?limit=N`
- `GET /api/activities/stream`（SSE）

## 4. 常见问题排查

### 4.1 请求未按预期走上游

- 检查规则顺序，是否被更前规则命中。
- 检查规则的 `upstream_id` 是否存在且上游已启用。
- 检查上游 URL 是否 `http://` 且可达。

### 4.2 Console 无法访问

- 确认访问地址是 `console_listen`。
- 检查端口占用。

### 4.3 BLOCK 状态码

- Web Console 下 `BLOCK` 状态码固定为 `404`。

## 5. 维护建议

每次改动后执行：

```bash
go test ./...
```

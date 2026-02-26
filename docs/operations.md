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

## 4. 数据备份与恢复

- 导出备份：`GET /api/data/backup`
- 恢复备份：`POST /api/data/restore`
- 导入 ABP：`POST /api/data/import-abp`（multipart: file + route_target）
- 恢复策略为全量替换（会清空当前 rules/upstreams 并按备份重建）
- 备份体包含 `data_format_version`，版本不兼容时会拒绝恢复

## 5. 常见问题排查

### 5.1 请求未按预期走上游

- 检查规则顺序，是否被更前规则命中。
- 检查规则的 `upstream_id` 是否存在且上游已启用。
- 检查上游 URL 是否 `http://` 且可达。

### 5.2 Console 无法访问

- 确认访问地址是 `console_listen`。
- 检查端口占用。

### 5.3 BLOCK 状态码

- Web Console 下 `BLOCK` 状态码固定为 `404`。

## 6. 维护建议

每次改动后执行：

```bash
go test ./...
```

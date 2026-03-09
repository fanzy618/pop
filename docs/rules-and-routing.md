# 规则与路由设计

## 1. 规则对象

规则包含以下关键字段：

- `id`：规则唯一标识（数据库自增主键）。
- `enabled`：是否启用。
- `pattern`：域名模式。
- `action`：`DIRECT` / `PROXY` / `BLOCK`。
- `upstream_id`：当 action 为 `PROXY` 时必填（上游数据库主键）。
- `block_status`：Web Console 固定为 `404`。

## 2. 匹配语义

- 匹配输入是归一化后的 host。
- 规则会匹配自身域名以及其所有子域名。
- 多条规则同时命中时，pattern 更长的规则优先。
- 若长度相同，则保持现有顺序，较新的规则优先。
- 未命中任何规则时走默认动作（当前默认 `DIRECT`）。

## 3. pattern 支持

### 3.1 域名后缀匹配

- 示例：`example.com`
- 匹配 `example.com`、`a.example.com`、`b.c.example.com`。
- 不匹配 `abc-example.com` 这类仅包含相似后缀但不在 label 边界上的域名。
- 历史规则 `*.example.com` 会按 `example.com` 兼容处理。

## 4. 动作语义

- `DIRECT`：POP 直接连目标主机。
- `PROXY`：POP 选择 `upstream_id` 对应上游 HTTP 代理进行转发。
- `BLOCK`：POP 直接返回错误响应，不对外发起请求。

## 5. 默认策略设计

当前默认策略使用 `DIRECT`，主要考虑：

- 更安全地避免内网流量误发到外部代理。
- 用户初次使用时行为更接近普通代理，理解成本低。

如需“默认走上游代理”，可以调整 `default_action` 并新增兜底规则。

## 6. 常见配置建议

- 将明确的内网规则写成根域（如 `corp.local -> DIRECT`）。
- 将更具体的域名规则写得更长（如 `abc.example.com -> DIRECT` 覆盖 `example.com -> PROXY`）。
- 将区域性外网规则按优先级分配到不同上游（A/B）。
- 最后依赖默认策略处理未覆盖域名。

## 7. 常见误配置

- `PROXY` 规则缺失 `upstream_id`：会导致请求失败。
- `*ads*` 这类包含匹配不再生效，应改成明确域名规则。
- 误以为较短规则会覆盖更具体规则：实际由更长 pattern 优先生效。

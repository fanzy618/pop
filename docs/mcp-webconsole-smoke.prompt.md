# MCP Web Console Smoke Script

将下面整段提示词直接发给 OpenCode，可触发一轮基于 Chrome MCP 的 POP Web Console 冒烟测试。

```text
请使用 chrome-devtools MCP 对 POP 的 web console 做一轮冒烟测试。

目标：验证 stats/activities/rules/upstreams 页面可用，且前后端联动正常。

执行要求：
1) 先用 Bash 启动 POP：go run ./cmd/pop -config ./pop.example.json
   - 后台运行
   - 等待 http://127.0.0.1:9090/api/stats 可访问（HTTP 200）
2) 使用 chrome-devtools MCP 新开页面并访问：
   - http://127.0.0.1:9090/stats
   - http://127.0.0.1:9090/activities
   - http://127.0.0.1:9090/rules
   - http://127.0.0.1:9090/upstreams
3) 在每个页面抓取 snapshot，并校验关键文本：
   - stats: “实时统计”
   - activities: “实时活动”
   - rules: “规则管理”
   - upstreams: “上游管理”
4) 通过 Bash 发送一条代理流量：
   - curl -x http://127.0.0.1:8080 http://example.com -I
   - 回到 stats 页面确认“总请求”增加
   - 回到 activities 页面确认出现 example.com 的活动记录
5) 在 rules 页面执行一次真实 CRUD：
   - 新增规则 id=mcp-smoke-rule, pattern=smoke.pop.local, action=DIRECT
   - 确认列表出现该规则
   - 删除该规则
   - 确认规则被移除
6) 在 upstreams 页面执行一次真实 CRUD：
   - 新增上游 id=mcp-smoke-up, url=http://127.0.0.1:18080, enabled=true
   - 确认列表出现该上游
   - 删除该上游
   - 确认上游被移除
7) 测试结束后清理：
   - 停止本次启动的 POP 进程

输出要求：
- 给出每一步是否通过（PASS/FAIL）
- 给出失败时的实际现象
- 最后给出总结果
```

## 说明

- 该脚本是“可执行提示词”，用于驱动 OpenCode + Chrome MCP 自动完成页面级冒烟验证。
- 使用默认配置账号 `admin/admin`（见 `pop.example.json`）。
- 如本机已有 POP 占用 `8080/9090`，请先停止旧进程后再执行。

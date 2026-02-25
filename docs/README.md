# POP 设计文档

本文档目录面向开发者与维护者，描述 Proxy of Proxy (POP) 的需求、架构、里程碑与测试策略。

## 阅读顺序

1. `docs/requirements.md`：需求与验收标准
2. `docs/architecture.md`：系统架构与关键流程
3. `docs/rules-and-routing.md`：规则匹配与路由决策细则
4. `docs/milestones.md`：里程碑目标、范围与完成定义
5. `docs/testing-strategy.md`：测试分层与回归策略
6. `docs/operations.md`：运行、配置与排障指南

## 当前实现状态

- M0-M6 已完成并通过测试闸门。
- MVP 默认策略为 `DIRECT`。
- MVP 上游代理仅支持 `HTTP proxy`。
- `BLOCK` 动作默认状态码为 `404`（规则可覆盖）。

# 实现日志

按《大闸蟹订单管理 — 后端技术方案》实现，记录进度、决策与遇到的问题。

---

## 环境

- Go 1.24.7（方案要求 1.22+，满足）
- `modernc.org/sqlite v1.34.5` 拉取成功，**无需**降级到 `mattn/go-sqlite3`，保持 CGO_ENABLED=0 纯 Go 构建。
- 仓库初始为空（仅 LICENSE/README），从零搭建。

## 进度

| 阶段 | 内容 | 状态 |
|---|---|---|
| P0 | config / slog / SQLite 连接与 migrate / 统一响应与错误码 / 中间件 / healthz | 进行中 |
| P1 | model 与枚举 / store / 订单号 / 建单·列表·详情·改单·软删 / 金额 / 幂等 | 待开始 |
| P2 | 发货 / 收货 / 取消 / 回退 / order_logs | 待开始 |
| P3 | 收款流水与 paid_amount、pay_status 重算 | 待开始 |
| P4 | 鉴权（code2session / token / Auth 中间件 / 引导模式） | 待开始 |
| P5 | 统计 / 发货计划 / 地址簿 / 规格价目表 / CSV 导出 | 待开始 |
| P6 | 买家免登录查单 / 脱敏 / IP 限流 | 待开始 |
| P7 | 测试 / README / Makefile / systemd / 备份脚本 | 待开始 |

## 决策与偏离说明

（方案第 3 章的目录结构为指导性描述，字段名/枚举/路由/错误码为强约定，未做任何改动。以下为在指导性部分上的补充。）

1. **新增 `internal/errs` 包**：方案把「统一响应包装与错误码」放在 `api/response.go`，但 service 层需要返回带业务码的错误，而 service 不应反向依赖 api（分层规则 handler → service → store 单向）。因此把错误码与 `*errs.Error` 类型抽到独立的 `internal/errs`，`api/response.go` 只负责业务码 → HTTP 状态码的映射与 JSON 包装。
2. **新增 `internal/api/dto.go`**：订单对象的 JSON 视图（含 `*_yuan`、`*_text` 派生字段）构造逻辑集中在这里，避免 handler 里重复拼装。

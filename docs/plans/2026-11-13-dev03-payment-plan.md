# DEV03 模拟支付实施计划

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 为待付款预约增加可审计的支付记录和 HMAC 鉴权模拟回调，覆盖成功、失败、超时、重复通知及关闭后迟到成功。

**Architecture:** `payments` 独立记录支付状态，`exceptions` 记录关闭/取消后迟到成功及待退款事实；回调只接受原始 JSON 的 HMAC-SHA256 `X-Signature`。处理回调时事务锁定支付和预约，成功通知按状态条件生效，重复通知返回原结果，不恢复已关闭/已取消预约。

**Tech Stack:** Go 1.26、`database/sql`、MySQL 8.0、HMAC-SHA256、`internal/payment`、`internal/store/mysql`、`internal/httpapi`。

---

### Task 1: 支付失败测试

- 新建 `internal/payment/service_test.go` 和 `internal/store/mysql/payment_integration_test.go`。
- 先覆盖支付意图、签名拒绝、成功确认、失败/超时不确认、重复通知幂等、关闭后迟到成功登记异常待退款。
- 运行针对性测试，确认因支付类型、表和方法缺失而失败。

### Task 2: 支付/异常持久化

- 在 `Migrate` 增加 `payments`、`exceptions` 表。
- 实现创建支付、HMAC 验证、事务回调处理和查询结果；金额固定从项目价格读取为 9900 分，不能由请求决定。
- 使用随机测试库验证重启/重复回调和异常记录。

### Task 3: HTTP 模拟渠道边界

- 增加用户创建支付意图接口和模拟渠道回调接口；回调密钥只从进程配置读取，不写入日志或响应。
- 未登录、跨站、伪造金额/预约归属、无签名/错误签名和重复回调必须拒绝或幂等。
- 运行 `go test ./...`、`go vet ./...`、Docker `go test -race ./...`。

### Task 4: 记录门禁

- 更新 `docs/dev03-progress.md`、`docs/dev03-rule-check.md`。
- 明确退款表和退款回调留给 DEV04；不提交、推送、合并或部署。

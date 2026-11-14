# 题目 02 本地修复复验报告

- 阶段：`main` 审查缺陷修复后的开发分支本地复验，非上线复验
- 输入版本：`dev/02` 基线 `d40faeb` 加本次未提交修复
- 验收时间：2026-11-14 09:12（北京时间）
- 题面：用户提供的 `Go项目业务场景题.md` 题目 02
- 决策表：`AGENTS.md` D01-D14
- 使用 Skill：`pet-appointment-rules`、`pet-appointment-acceptance`
- 结论：**本次发现的四项非部署缺陷已修复并通过本地复验；提交后须对确定提交重新执行门禁。**

## 缺陷复验

| 编号 | 原缺陷 | RED | 修复 | GREEN |
|---|---|---|---|---|
| F01 | 过期待付款仍占位，且成功回调可确认过期预约 | 新预约返回容量满；过期支付没有异常/退款记录 | 新建预约时在时段锁内关闭到期占位；支付回调同时锁定并检查 `expires_at` | 自动释放后新预约成功；过期支付保持 `CLOSED`，创建唯一异常和 9900 分待退款 |
| F02 | `COMPLETED` 不计入时段容量 | 完成一个预约后第 4 个预约成功 | 容量占用状态加入 `COMPLETED` | 第 4 个预约返回 `ErrCapacityFull` |
| F03 | MySQL 集成覆盖率被当作单元覆盖率 | 不设置测试 DSN 时存储包仅 14.1%（修复前） | 增加数据库独立 SQL 仓储单测并分开报告口径 | 纯单元整体 85.0%，存储包 86.2%，核心包均 >=85% |
| F04 | 取消申请审计失败时事务未立即回滚 | 单连接池后续查询超时 | `BeginTx` 成功后立即 `defer tx.Rollback()` | 审计失败后同一连接可立即查询 |

## 规则复核

| 项目 | 结果 | 证据 |
|---|---|---|
| 5 分钟占位和名额循环 | 通过 | `TestCreatingBookingClosesExpiredPendingCapacity` |
| 支付与超时竞争、迟到支付退款 | 通过 | `TestLatePaymentAtExpiredTimestampClosesBookingAndCreatesRefund` 及原迟到支付测试 |
| 每时段容量不因完成服务重开 | 通过 | `TestCompletedBookingStillConsumesSlotCapacity` |
| 事务失败回滚 | 通过 | `TestCancellationRequestAuditFailureReleasesTransactionConnection`、SQL 故障注入单测 |
| 取消、核销、履约、退款、日清既有规则 | 通过 | 真实 MySQL 全量测试与 Docker race 回归 |

## 质量结果

- 纯单元：整体 `85.0%`；所有核心业务包 `>=85%`。
- 真实 MySQL：全部通过；整体覆盖率 `88.1%`。
- `go vet`、构建、原型 7 项、Docker `-race`：通过。
- 工作区差异：仅本次生产修复、测试、依赖和证据文档；未修改部署配置和学生决策。

## 未覆盖范围

- 正式提交哈希、PR、合并后流水线和部署版本尚不存在，不能沿用本工作区结论冒充发布验收。
- 正式 HTTPS 部署、线上完整冒烟、回退演练、教师评分 Skill、视频和平台会话导出仍按原约束另行完成。

## 复验范围

代码提交后重新运行纯单元覆盖率、真实 MySQL 全量测试、`go vet`、构建、原型及 Docker race；如提交内容与本工作区不同，重新检查受影响规则。

# Skill 实际使用记录

## pet-appointment-rules

- 来源与版本：项目自编 Skill，`.agents/skills/pet-appointment-rules/SKILL.md`；仓库初始版本见提交 `9d322ee`。
- 使用位置：需求决策 Q01–Q04、DEV01–DEV07，以及本次非部署最终规则复核；各阶段记录见 `AGENTS.md` 第 10 节和 `docs/*-rule-check.md`。
- 使用原因：检查容量、支付回调、取消边界、退款、核销/履约、并发竞争与日清资金是否符合题面及 D01–D14。
- 实际过程：输入题目 02 原文、决策表、当前源码和测试；逐项核对 R01–R12。最终复核发现关闭预约后的迟到支付只写 `LATE_PAYMENT/NEEDS_REFUND` 异常，没有创建 `refunds` 待退款记录。
- 实际帮助：新增失败测试复现“迟到支付无退款记录”，随后在同一支付回调事务中创建唯一的 9900 分 `PENDING` 退款；补充退款写入失败时整笔回滚测试。支付、重复通知、日清和 MySQL 全量复测通过。
- 证据：`internal/store/mysql/payment.go`、`internal/store/mysql/payment_integration_test.go`、本次会话 RED/GREEN 输出。
- 局限：Skill 只核对业务规则，不替代独立验收、部署流水线或教师评分。

## pet-appointment-acceptance

- 来源与版本：项目自编 Skill，`.agents/skills/pet-appointment-acceptance/SKILL.md`；仓库初始版本见提交 `9d322ee`。
- 使用位置：DEV06/DEV07 合并前审计、PR 合并后的临时上线核对、本次非部署最终验收。
- 使用原因：按原题检查角色权限、五组场景、持久化、覆盖率、竞争检测和证据完整性，避免把开发 Agent 声明当作验收结论。
- 实际过程：核对原始题面、D01–D14、源提交/PR 关系、源码、数据库测试、页面入口、覆盖率 profile、race 输出和上线记录。
- 实际帮助：此前识别页面缺少时段发布和逐笔操作入口、存储层覆盖率不足、审计表缺失；本次完成时段发布、角色工作台、事务审计和全部包 85% 门禁后重新核验。
- 证据：`docs/acceptance-dev06-progress.md`、`docs/acceptance-dev07-progress.md`、`docs/acceptance-final-nondeployment.md`、`docs/coverage-2026-11-13.md`。
- 局限：正式部署、可信证书、流水线和线上完整业务冒烟仍不属于本次通过范围。

## 外部评分 Skill

教师评分 Skill 尚未提供，因此未调用、未预填名称或分数。学生取得 Skill 后必须对实际提交版本运行并保存名称、版本、输入、输出、评分依据、修正和复测记录。

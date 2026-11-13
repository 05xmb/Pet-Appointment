# 非部署交付缺口补齐实施计划

> **For Codex:** 按测试先行逐项执行本计划；部署、可信证书和自动发布不在本计划范围内。

**目标：** 补齐题目 02 在部署之外仍缺失的时段发布、可操作工作台、审计记录、覆盖率门禁和交付证据。

**架构：** 沿用现有 Go HTTP 单体、`database/sql` 与 MySQL 事务。店长通过会话身份创建时段，时段和审计记录持久化；页面按服务端返回的角色展示操作并渲染结构化查询结果。所有状态变化在原有事务中追加审计记录，不改变既有状态机。

**技术栈：** Go 1.26、标准库 `net/http`、MySQL 8、现有业务包与 Node 原型测试。

---

### 任务 1：时段发布与查询

**文件：**
- 修改：`internal/store/mysql/auth.go`、新增 `internal/store/mysql/slots.go`
- 修改：`internal/httpapi/auth.go`、`internal/httpapi/web.go`
- 测试：`internal/store/mysql/slots_integration_test.go`、`internal/httpapi/slots_test.go`

**验收：** 店长可创建未来时段；起止时间使用 `Asia/Shanghai` 解析；开始或结束非法、非店长、跨门店和重复请求被拒绝；主人可查询可预约时段。

### 任务 2：审计日志

**文件：**
- 修改：`internal/store/mysql/auth.go` 迁移
- 新增：`internal/store/mysql/audit.go`
- 修改：预约、支付、取消、履约和时段写事务
- 测试：审计表存在、关键状态变更各至少一条记录、回滚不留下孤立日志

**验收：** 能按实体、操作者、动作和时间查询关键业务记录；审计写入与业务事务同提交。

### 任务 3：完整轻量工作台

**文件：**
- 修改：`internal/httpapi/web.go`
- 修改：`internal/httpapi/auth.go` 查询接口（仅补充页面需要的只读字段）
- 测试：`internal/httpapi/web_test.go` 和角色操作页面结构断言

**验收：** 页面提供店长发布时段、时段/预约查询、主人预约/支付、前台核销、服务开始/完成、客服审核/退款、店长未到店和逐笔日清结果；错误展示来自服务端响应。

### 任务 4：覆盖率和质量门禁

**文件：**
- 新增/修改测试：`cmd/pet-appointment/*_test.go`、核心包测试
- 新增：`docs/coverage-2026-11-13.md`

**验收：** 核心包单元覆盖率和整体覆盖率分别有原始命令与输出；MySQL 集成覆盖率单独标识；格式化、`go vet`、可用环境下的 race 检查均有记录。

### 任务 5：交付证据收口

**文件：**
- 新增：`docs/acceptance-final-nondeployment.md`
- 新增：`docs/skill-usage.md`
- 修改：`AGENTS.md` 实施记录
- 修改：`README.md` 完成状态和运行说明

**验收：** 明确哪些项目已完成、哪些只属于部署/教师外部动作；不虚构不可获取的会话、评分或流水线证据。

# DEV02 预约占位实施计划

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 实现题目 02 的预约占位、同宠物去重、容量竞争、5 分钟未付款关闭和主动取消，并让结果持久化可复查。

**Architecture:** 在 MySQL 增加 `bookings` 表，预约主状态只保存当前事实，支付/退款后续由 DEV03/DEV04 独立建模。创建、超时关闭和主动取消均在事务中锁定时段/预约，使用条件更新保证名额最多释放一次；教学时间由 `app_clock` 偏移计算，正式 HTTP 入口后续再接会话身份。

**Tech Stack:** Go 1.26、`database/sql`、MySQL 8.0、`internal/store/mysql`、`internal/booking`。

---

### Task 1: 预约规则失败测试

**Files:**
- Create: `internal/booking/service_test.go`
- Create: `internal/store/mysql/booking_integration_test.go`

**Step 1: Write failing tests**

覆盖：成功占位、同宠物重复、时段已开始、容量 3 个上限、超时关闭只释放一次、主动取消只释放一次、取消后可再次预约、并发抢最后名额只有一个成功。

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/booking ./internal/store/mysql -run Booking -v`

Expected: FAIL because booking repository/schema/service do not exist.

### Task 2: 数据表和持久化操作

**Files:**
- Modify: `internal/store/mysql/auth.go` migration list
- Create: `internal/store/mysql/booking.go`

**Step 1: Implement schema**

增加 `bookings`：预约编号、时段、宠物、主人、状态、过期时间、创建/更新时间；外键关联 `slots`、`pets`、`accounts`；有效状态查询使用索引。用唯一约束阻止同宠物同一时段的有效预约无法直接表达时，事务内锁定宠物/时段并查询状态。

**Step 2: Implement transactional methods**

实现创建、关闭过期预约、主动取消和按主人查询。创建时锁定 slot，读取教学时钟，拒绝已开始、非本人宠物、重复有效预约和容量不足；过期/取消使用 `WHERE status = 'PENDING_PAYMENT'` 条件更新。

**Step 3: Run integration tests**

Run: `PET_MYSQL_TEST_DSN=... go test ./internal/store/mysql -run Booking -v`

Expected: PASS in a random isolated test database; no changes to `宠物门店`.

### Task 3: 业务服务和 HTTP 边界

**Files:**
- Create: `internal/booking/service.go`
- Modify: `internal/httpapi/auth.go`
- Create: `internal/httpapi/booking_test.go`

**Step 1: Implement service errors and methods**

将存储层错误映射为可操作的冲突/非法状态结果；身份只来自会话，不接受请求中的 `user_id` 作为授权依据。

**Step 2: Add endpoints**

增加登录后用户的预约、我的预约、取消和清理超时接口；错误密码、未登录、他人宠物和伪造用户参数都应被拒绝。状态变更继续经过跨站请求保护。

**Step 3: Verify**

Run: `gofmt -w ...; go test ./...; go vet ./...; go test -race ./...`（Windows 用 Go 1.26 Docker）。

### Task 4: 记录和门禁

更新 `docs/dev02-progress.md` 和 `docs/dev02-rule-check.md`，记录失败测试、数据库并发结果、覆盖率及未实现的 DEV03–DEV06；不提交、推送、合并或部署。

# DEV07 覆盖率补齐与复核记录

- 时间：2026-11-13（北京时间）
- 分支：`dev/02`
- 范围：补齐 MySQL 存储层异常/权限/状态分支，并复核独立只读审查提出的认证并发与登录限速问题。

## 覆盖率补齐

新增 `internal/store/mysql/edge_integration_test.go`，覆盖：

- 预约时段、宠物和预约归属不存在/越权、关闭和取消的非法状态。
- 支付创建、回调对象不匹配、金额/预约不存在、关闭预约支付。
- 取消时间边界、客服拒绝/重复审核、无退款记录和未知退款状态。
- 核销、服务、未到店的不存在对象、角色、门店、时间窗和状态错误。
- 数据库连接关闭时各业务事务入口的错误返回，以及履约事件唯一约束失败后的事务回滚。

命令：

```powershell
$env:PET_MYSQL_TEST_DSN='user:<password>@tcp(127.0.0.1:3306)/?parseTime=true'
go test -count=1 -coverprofile coverage.out ./internal/store/mysql
go tool cover -func coverage.out
```

结果：`internal/store/mysql` 真实 MySQL 集成覆盖率 `85.0%`，测试通过。

## 只读复核反馈核验

复核提出“密码变更与登录可能交错产生旧会话”。核对后不复现：`SaveSession` 和 `ProvisionPassword` 均对账号行使用 `SELECT ... FOR UPDATE`；保存会话时再次比较密码散列，密码变更提交后删除该账号已有会话。`TestPasswordResetBetweenVerificationAndSessionWriteRejectsOldLogin` 已覆盖更新先于会话写入的门控竞争，`TestTeachingSeedIsIdempotentAndPasswordsAreProvisionedSeparately` 覆盖更新后的旧会话失效。

复核提出“成功登录也计入登录限速”。核对后不成立：限速键是来源 IP 与用户名，登录成功后调用 `succeeded` 删除窗口，错误凭据不调用清零；`TestLoginLimiterTracksFailuresAndExpiresWindows` 和 HTTP 登录限速用例已覆盖失败累计、成功清零和窗口过期。

本次未修改认证或限速生产代码。

## 回归验证

以下命令均通过：

- `go test -count=1 ./...`（设置 `PET_MYSQL_TEST_DSN`）
- `go vet ./...`
- `go build ./cmd/pet-appointment`
- `node --test prototype/prototype.test.cjs`（7 项）
- Docker Go 1.26：`go test -race -count=1 ./...`

## 仍待完成

- 原始题目附件在当前环境仍不可读，需保留为验收阻塞。
- 正式地址仍为明文 HTTP，不能承载账号密码和安全会话 Cookie。
- 工作区尚未形成确定提交，尚无 PR、主分支流水线、合并后部署和线上冒烟证据。

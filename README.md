# Pet-Appointment

题目 02「宠物门店的预约、接待、服务与取消退款」。当前完成 DEV01 基础、DEV02 预约占位、DEV03 模拟支付、DEV04 取消/退款、DEV05 核销/履约/未到店，以及 DEV06 最小查询/日清接口和操作页面；完整工作台、覆盖率门禁和正式发布仍未完成，不能把当前版本当作最终上线版本。

## 本地初始化

需要 Go 1.26 或更高版本、MySQL 8.0 和已创建的 `宠物门店` 数据库。将 `PET_MYSQL_DSN` 设在当前进程环境中，数据库名采用 URL 转义；示例结构为 `user:<password>@tcp(127.0.0.1:3306)/%E5%AE%A0%E7%89%A9%E9%97%A8%E5%BA%97?parseTime=true`。不要把真实 DSN、口令或会话令牌写入仓库、命令历史或日志。

```powershell
go run ./cmd/pet-appointment init
go run ./cmd/pet-appointment set-password owner1
```

`init` 幂等创建表及一家门店、一个洗护项目（9900 分）、4 位主人/宠物、3 个未来时段和 4 位员工账号。全部账号初始无密码散列，不能登录；逐个执行 `set-password`，在交互终端隐藏输入并确认密码。账号为 `owner1`、`owner2`、`owner3`、`owner4`、`frontdesk`、`groomer`、`support`、`manager`。`serve` 会拒绝未初始化或仍有账号未设密码的数据库。

服务还需要 `PET_TLS_CERT`、`PET_TLS_KEY` 和 `PET_PAYMENT_HMAC_SECRET`；可选 `PET_LISTEN_ADDR`，默认 `127.0.0.1:8443`。运行 `go run ./cmd/pet-appointment serve` 后，当前提供 `GET /` 最小操作页面、`POST /login`、`GET /me`、`POST /logout`、`GET /clock`、店长专用 `POST /clock/advance`，`GET /bookings?date=YYYY-MM-DD`、店长专用 `GET /daily-summary?date=YYYY-MM-DD`，DEV02 的 `POST /bookings`、`POST /bookings/{id}/cancel`、店长专用 `POST /bookings/{id}/expire`，DEV03 的 `POST /payments`、`POST /payments/callback`，以及 DEV04–DEV05 的 `POST /bookings/{id}/cancel-request`、`POST /cancellation-requests/{id}/review`、`POST /refunds/callback`、`POST /bookings/{id}/check-in`、`POST /bookings/{id}/service/start`、`POST /bookings/{id}/service/complete`、`POST /bookings/{id}/no-show`。登录接受表单字段 `username`、`password`；预约接受 `pet_id`、`slot_id`，取消申请/未到店接受 `reason`，客服审核接受 `approve=true|false`，支付和退款回调接受原始 JSON 和 `X-Signature`。接口从服务端会话取身份，Cookie 为 `Secure`/`HttpOnly`/`SameSite=Lax`，明文 HTTP 请求被拒绝。

目前提供的正式教学地址仍为 HTTP，不能直接用它传输账号密码或会话 Cookie；需要可信 HTTPS/TLS 入口后才能上线登录。此处不附带证书、密码或部署成功声明。

## 检查

```powershell
go test ./...
go vet ./...
node --test prototype/prototype.test.cjs
```

MySQL 集成测试需要临时设置 `PET_MYSQL_TEST_DSN`，指向有权创建测试库的本机连接；测试会创建随机命名的 `pet_appointment_test_<hex>` 数据库，并在结束时删除该测试库，不操作 `宠物门店`。未设置时测试会明确跳过。最终质量门禁还需整体与核心包单测覆盖率 ≥85%、`go test -race ./...`、独立测试/验收及主分支部署证据；当前阶段未达到这些门禁。

当前 Windows Go 环境缺少 CGO 编译器，可用 Docker 的 Go 1.26 镜像在不带数据库凭证时运行竞争检测（MySQL 集成用例会跳过）：

```powershell
$workspacePath = (Get-Location).Path
docker run --rm -v "${workspacePath}:/app:ro" -w /app golang:1.26 go test -race ./...
```

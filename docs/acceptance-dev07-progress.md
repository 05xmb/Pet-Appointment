# 题目 02 合并前验收补充

- 阶段：合并前本地验收补充
- 验收版本：`dev/02` 提交 `83d67a9`，已推送到 `origin/dev/02`
- 验收时间：2026-11-13（北京时间）
- 结论：**局部通过，仍阻塞最终合并/上线验收**。

## 本次复验

| 项目 | 命令/证据 | 结果 |
|---|---|---|
| MySQL 存储层分支覆盖 | `PET_MYSQL_TEST_DSN=... go test -count=1 -coverprofile coverage.out ./internal/store/mysql`；`go tool cover -func coverage.out` | 通过，覆盖率 `85.0%` |
| 全量 Go 回归 | `PET_MYSQL_TEST_DSN=... go test -count=1 ./...` | 通过 |
| 静态检查和构建 | `go vet ./...`；`go build ./cmd/pet-appointment` | 通过 |
| 原型规则检查 | `node --test prototype/prototype.test.cjs` | 通过，7 项 |
| 竞争检测 | Docker Go 1.26：`go test -race -count=1 ./...` | 通过 |
| 认证并发/限速反馈复核 | `docs/dev07-coverage-and-review.md` 及既有门控测试 | 两项反馈均未在当前实现中复现，不构成新缺陷 |

## 仍然阻塞

| 编号 | 原因 | 证据 |
|---|---|---|
| A-01 | 用户指定的原始题面与提交规范附件在当前环境不可读，不能声称已按原文逐项核验 | `docs/acceptance-dev06-progress.md` |
| A-04 | 正式教学地址仍为明文 HTTP；账号密码登录和 `Secure` 会话 Cookie 不能安全部署到该入口 | `AGENTS.md` 部署前安全阻塞、`README.md` |
| A-05 | 已有确定开发提交并推送，但当前环境没有 `gh`、GitHub Token 或已登录网页会话，PR 尚未创建；因此也没有主分支流水线、合并后部署和线上冒烟记录 | `git log --oneline`、`git push` 输出、GitHub compare 页面 |
| A-06 | 页面仍是最小操作工作台，未覆盖完整角色导航、时段发布和逐笔日清表格 | `docs/dev06-progress.md` |

覆盖率补齐不等于题目最终验收通过；解决原文来源和上线安全入口后，需要在确定提交及实际发布版本上分别复验。

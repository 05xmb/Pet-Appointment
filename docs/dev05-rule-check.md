# 业务规则检查结果：DEV05 核销与履约

- 检查版本：`dev/02` 未提交工作区；DEV01–DEV05 代码和测试
- 题面版本：用户提供的 `Go项目业务场景题.md` 题目 02（原文件未标版本号）
- 决策表版本：`AGENTS.md` v2（待提交），D04–D07 已由学生确认
- 结论：**存储规则局部通过，整体阻塞**。核销、服务、未到店及数据库竞争已有集成证据；页面、完整角色权限、日清和独立验收仍未完成。

## 检查项

| 编号 | 规则 | 结果 | 证据 | 缺陷/阻塞 |
|---|---|---|---|---|
| R08 / D04 | 仅本店前台在开始前 30 分钟至结束核销已确认预约 | 通过 | `TestCheckInWindowRoleAndPendingCancellation` | 完整跨店数据夹具和 HTTP 权限待 DEV06 |
| R08 / D06 | 只能按核销→开始→完成；结束后已开始服务可补完成 | 通过 | `TestServiceLifecycleAllowsCompletionAfterEnd` | 页面查询待 DEV06 |
| R10 / D07 | 店长在时段结束后填写未到店原因；不自动退款 | 通过 | `TestManagerCanMarkNoShowOnlyAfterEnd` | 日清金额/结果展示待 DEV06 |
| R09 / D05 | 取消与核销竞争只有一个合法结果 | 通过 | `TestCancellationAndCheckInCompeteWithOneWinner` | HTTP 409 映射待 DEV06 |
| R10 / D12 | 取消申请与未到店竞争只有一个合法结果；待审取消不可标未到店 | 通过 | `TestCancellationAndNoShowCompeteWithOneWinner` | 页面状态提示待 DEV06 |
| R11–R12 | 日清、查询、完整角色权限和持久化重启后的可见结果 | 阻塞 | `docs/tasks.md` DEV06 | 尚无页面/查询接口和最终验收证据 |

## 复测范围

DEV06 接入 HTTP、页面和日清后，重新执行五组场景、角色越权、重启查询和金额对账；最终独立验收必须针对确定提交重新执行。

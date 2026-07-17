# ack task

资源：异步任务

优先级：P1

本文件只描述 `ecctl ack task` 的 interface 级命令设计：每个操作命令对应哪些 ACK API，不展开完整 flag、参数结构和输出结构；仅在多 API 分流时标明必要的特殊开关。

设计目标：ACK 绝大多数 mutation API（集群创建/删除/升级、节点池扩缩容/升级/修复、组件升级、漏洞修复等）默认返回 `task_id` 并由 spec 中的 waiter 等待终态，普通用户感知不到 task 模型；`task` 资源面只服务于两个高级场景：

1. 用户在 mutation 命令上显式指定 `--no-wait` 拿到 `task_id` 后，希望跟踪进度或在外部编排里拉状态。
2. 长时间任务（集群升级、批量节点修复等）希望中途暂停、恢复或取消。

`pause` / `resume` / `cancel` 仅对支持这些动作的 task 类型有效，是否支持由服务端裁定；不支持的 task 类型 API 会直接报错，CLI 不做客户端拦截，原样回传错误。

特殊开关命名是 interface 级建议，用来说明何时触发额外 API；最终字段名和参数细节在资源 spec 中定义。

## `ecctl ack task get`

调用 API：

- [DescribeTaskInfo](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-describetaskinfo)：查询单个 task 的详情，包括状态、当前阶段、错误信息、关联资源等。

注意事项：`get` 是只读查询。资源命令默认等待时也走 `DescribeTaskInfo` 轮询；这里仅给高级用户在显式 `--no-wait` 工作流里手动查询使用。`get` 不内置 watch/follow，需要持续观察由 shell 循环或上层编排处理。

## `ecctl ack task list`

调用 API：

- [DescribeClusterTasks](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-describeclustertasks)：按集群列出 task。

注意事项：`list` 必须指定集群；ACK 没有跨集群 task 列表 API。状态、类型等过滤项透传给 `DescribeClusterTasks` 的查询参数，不单独设计 `task status` 子命令。

## `ecctl ack task pause`

调用 API：

- [PauseTask](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-pausetask)：暂停一个执行中的 task。
- [DescribeTaskInfo](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-describetaskinfo)：回读 task 状态确认进入已暂停态。

注意事项：暂停是异步操作，默认等待 task 进入已暂停态后返回。是否允许暂停由服务端按 task 类型裁定，不支持时 `PauseTask` 直接报错，CLI 原样回传，不做客户端拦截。

## `ecctl ack task resume`

调用 API：

- [ResumeTask](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-resumetask)：恢复一个已暂停的 task。
- [DescribeTaskInfo](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-describetaskinfo)：回读 task 状态确认重新进入运行态。

注意事项：恢复是异步操作，默认等待 task 离开已暂停态后返回，但不等待 task 整体完成；后续如需等待完成由调用方再发起 `task get` 轮询或在原 mutation 命令上去掉 `--no-wait`。是否允许恢复由服务端按 task 类型裁定，不支持时 `ResumeTask` 直接报错，CLI 原样回传。

## `ecctl ack task cancel`

调用 API：

- [CancelTask](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-canceltask)：取消一个 task。
- [DescribeTaskInfo](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-describetaskinfo)：回读 task 状态确认进入已取消终态。

注意事项：取消是异步操作且不可恢复，默认等待 task 进入已取消终态后返回。已经进入成功/失败终态的 task 不能取消，由服务端报错，CLI 不做客户端拦截。是否允许取消由服务端按 task 类型裁定。

## 废弃/不推荐 API

以下 API 在阿里云官方文档已标记为废弃，已被通用 task 模型取代，迁移到 `ecctl ack task ...`，`ecctl ack` 不接入：

- [GetUpgradeStatus](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-getupgradestatus)：查询集群升级状态，已由通用 `DescribeTaskInfo` 取代，迁移到 `ecctl ack task get`。
- [PauseClusterUpgrade](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-pauseclusterupgrade)：暂停集群升级，已由通用 `PauseTask` 取代，迁移到 `ecctl ack task pause`。
- [ResumeUpgradeCluster](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-resumeupgradecluster)：恢复集群升级，已由通用 `ResumeTask` 取代，迁移到 `ecctl ack task resume`。
- [CancelClusterUpgrade](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-cancelclusterupgrade)：取消集群升级，已由通用 `CancelTask` 取代，迁移到 `ecctl ack task cancel`。

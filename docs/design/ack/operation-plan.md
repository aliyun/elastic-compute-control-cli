# ack operation-plan

资源：自动运维计划

优先级：P2

别名：`op`

本文件只描述 `ecctl ack operation-plan` 的 interface 级命令设计：每个操作命令对应哪些 ACK API，不展开完整 flag、参数结构和输出结构；仅在多 API 分流时标明必要的特殊开关。

设计目标：覆盖自动运维计划（控制面/节点池等定时维护任务）的查询与取消。

- 不设计 `create` / `update` / `delete`：运维计划由 ACK 平台基于集群与节点池的维护窗口、版本生命周期等状态自动派生，CLI 不参与计划本身的生成与编辑，相关变更在控制台或 ACK 平台侧完成。
- `cancel` 仅对尚未执行的运维计划有效；已开始或已完成的计划不再可取消，需要走任务面或工单。
- `list` 默认列出当前账号下所有运维计划，按地域聚合查询时通过 `--by-region` 切到 `ListOperationPlansForRegion`。
- `get` 复用 `ListOperationPlans`，通过运维计划 ID 收敛到单条详情；ACK 未提供独立的单计划详情 API。

特殊开关命名是 interface 级建议，用来说明何时触发额外 API；最终字段名和参数细节在资源 spec 中定义。

## `ecctl ack operation-plan get`

调用 API：

- [ListOperationPlans](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-listoperationplans)：复用列表 API，通过运维计划 ID 过滤到单条详情。

注意事项：ACK 未提供 `GetOperationPlan` 这类单查接口，`get` 通过 `ListOperationPlans` + plan-id 过滤兜底，与 `list` 共用底层调用、语义按读取主详情区分，与 ecs `command get` 风格一致。

## `ecctl ack operation-plan list`

调用 API：

- 默认调用 [ListOperationPlans](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-listoperationplans)：列出当前账号下的运维计划。
- 指定 `--by-region` 时调用 [ListOperationPlansForRegion](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-listoperationplansforregion)：按地域列出运维计划。

注意事项：默认走账号级聚合 `ListOperationPlans`；只有在用户明确按地域筛查时才通过 `--by-region` 切到 `ListOperationPlansForRegion`，避免对同一资源面提供两个并列子命令。两个 API 的返回字段以服务端实际响应为准，由 spec 适配为统一视图。

## `ecctl ack operation-plan cancel`

调用 API：

- [CancelOperationPlan](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-canceloperationplan)：取消尚未执行的运维计划。

注意事项：`cancel` 是领域动作，仅对未执行的计划有效；对已开始或已完成的计划调用会被服务端拒绝，CLI 透传错误并提示走任务或工单路径。取消是同步操作，默认回读对应计划状态确认进入已取消终态。

## 暂不进入主命令面的 API

无。运维计划当前仅有 `ListOperationPlans` / `ListOperationPlansForRegion` / `CancelOperationPlan` 三个 API，已全部纳入主命令面。

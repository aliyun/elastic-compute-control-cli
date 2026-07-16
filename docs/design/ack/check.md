# ack check

资源：集群检查

优先级：P1

别名：无

本文件只描述 `ecctl ack check` 的 interface 级命令设计：每个操作命令对应哪些 ACK API，不展开完整 flag、参数结构和输出结构；仅在多 API 分流时标明必要的特殊开关。

设计目标：覆盖集群"变更前预检"场景，包括升级前检查、迁移前检查、组件兼容性校验等专项任务。一次 check 等于一份检查报告。

`check` 与 `inspect` / `diagnosis` 的语义边界：

- `check`：在执行某项变更（升级、迁移等）之前发起的预检，输出一份本次变更可行性的检查报告，是面向变更生命周期的一次性任务。
- `inspect`：周期性巡检，沉淀集群健康度和最佳实践基线，由 `inspect config` 控制周期，由 `inspect run` 触发即时巡检。
- `diagnosis`：针对已经出现的问题做专项诊断（节点、网络、负载等），是面向已知症状的故障排查。

`check` 是任务型资源：`create` 创建一次新检查并默认回读结果，`get` / `list` 查询历史；ACK 不提供修改和删除检查记录的 API，因此没有 `update` / `delete`。

## `ecctl ack check create`

调用 API：

- [RunClusterCheck](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-runclustercheck)：发起一次集群检查，需指定检查类型（升级前、迁移前、组件兼容性等）。
- [GetClusterCheck](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-getclustercheck)：回读检查结果。

注意事项：`check create` 表达"创建一次新检查任务"，与 `ack inspect report create` / `ack vuls create` / `ack diagnosis create` 等异步任务创建语义对齐，不再使用 `check run`。`RunClusterCheck` 是异步接口，默认等待检查任务进入终态并通过 `GetClusterCheck` 回读检查报告；指定 `--no-wait` 时只返回 `check_id`，由 `ack task` 命令面跟进或调用方再次 `check get` 拉取。检查类型作为 `create` 的必填参数表达，不为每种类型单独设计子命令。

## `ecctl ack check get`

调用 API：

- [GetClusterCheck](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-getclustercheck)：查询某次检查的详细结果，包括各检查项的通过/失败状态及修复建议。

## `ecctl ack check list`

调用 API：

- [ListClusterChecks](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-listclusterchecks)：列出集群历史检查记录。

注意事项：列表只返回检查任务摘要（id、类型、状态、时间），详细结果通过 `get` 查询。

## 暂不进入主命令面的 API

当前 `ecctl ack check` 已覆盖检查场景的全部已知 API，暂无需要兜底的项。

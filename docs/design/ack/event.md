# ack event

资源：事件

优先级：P1

本文件只描述 `ecctl ack event` 的 interface 级命令设计：每个操作命令对应哪些 ACK API，不展开完整 flag、参数结构和输出结构；仅在多 API 分流时标明必要的特殊开关。

设计目标：覆盖 ACK 控制面事件（集群创建、升级、节点池变更、组件操作等的审计性事件流）的查询能力。三个 Describe API（`DescribeClusterEvents` / `DescribeEventsForRegion` / `DescribeEvents`）全部归入 `list`，按 flag 选择正确的后端，不拆成多个子命令。本资源只覆盖 ACK 控制面事件，不暴露 Kubernetes `Event` 资源（`kubectl get events`），后者属于 K8s 资源面，由 `kubectl` 操作。事件是只读审计流，没有 mutation 语义，因此不设计 `create` / `update` / `delete`。

特殊开关命名是 interface 级建议，用来说明何时触发额外 API；最终字段名和参数细节在资源 spec 中定义。

## `ecctl ack event list`

调用 API：

- 默认调用 [DescribeClusterEvents](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-describeclusterevents)：按集群列出事件，需要 `--cluster <id>`。
- 指定 `--by-region` 时调用 [DescribeEventsForRegion](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-describeeventsforregion)：按地域列出全部集群的事件，用于跨集群审计场景。
- 指定 `--type <type>` 或 `--source <source>` 时调用 [DescribeEvents](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-describeevents)：按事件类型或来源聚焦某类事件。

注意事项：默认入口是按集群查询，因此 `--cluster` 是常规路径所需参数；指定 `--by-region` 切换为地域级别审计视图，与 `--cluster` 互斥；指定 `--type` 或 `--source` 时走聚合查询接口，可与 `--cluster` 组合用于在单集群中按类型/来源过滤。三个后端 API 仍是同一 `list` 命令的内部分流，不单独设计 `events-for-region` 或 `events` 子命令。`--by-region` 命名与 `ack operation-plan list --by-region` 对齐。

# ack node

资源：节点

优先级：P0

别名：无

本文件只描述 `ecctl ack node` 的 interface 级命令设计：每个操作命令对应哪些 ACK API，不展开完整 flag、参数结构和输出结构；仅在多 API 分流时标明必要的特殊开关。

设计目标：覆盖节点级查询、删除和裸 attach 这三条最小必要路径。节点池侧的扩缩容、attach、移除是节点变更的主路径，归 [nodepool.md](nodepool.md) 承载；`ack node` 资源只承载查询、删除和无节点池场景下的裸 attach。Kubernetes 节点的 cordon、drain、taint、label 是 K8s 资源面能力，由 Agent 通过 `kubectl` 操作，不在 ACK 控制面 CLI 中重复实现。

特殊开关命名是 interface 级建议，用来说明何时触发额外 API；最终字段名和参数细节在资源 spec 中定义。

## `ecctl ack node delete`

调用 API：

- [DeleteClusterNodes](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-deleteclusternodes)：从集群移除节点。
- [DescribeClusterNodes](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-describeclusternodes)：确认节点已从集群中移除。

注意事项：`DeleteClusterNodes` 同时承载 release 与 detach 两种语义，由 `--release` 开关控制：默认仅从集群中 detach，保留底层 ECS 实例；指定 `--release` 时连同释放 ECS 实例。这是异步操作，默认等待节点从集群视图中消失。删除前的 cordon / drain 由调用方通过 `kubectl` 完成，不在该命令内置。

## `ecctl ack node get`

调用 API：

- [DescribeClusterNodes](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-describeclusternodes)：按节点 ID 查询单个节点详情。

注意事项：ACK 没有独立的单节点详情 API，`DescribeClusterNodes` 同时承担列表与单查；`get` 复用该 API，通过节点 ID 收敛到单个节点，与 `list` 共用底层调用、语义按读取主详情区分。

## `ecctl ack node list`

调用 API：

- 默认调用 [DescribeClusterNodes](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-describeclusternodes)：列出集群节点。
- 指定 `--nodepool` 时仍调用 [DescribeClusterNodes](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-describeclusternodes)：按节点池过滤节点列表。

注意事项：节点列表是集群视角的扁平视图；按节点池过滤通过 `--nodepool` 表达，不单独设计 `ecctl ack nodepool nodes list` 命令。

## `ecctl ack node attach`

调用 API：

- [AttachInstances](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-attachinstances)：将已有 ECS 实例直接加入集群（不指定节点池）。
- [DescribeClusterNodes](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-describeclusternodes)：回读节点加入后的状态。

注意事项：仅覆盖历史的"无节点池 attach"路径，仅适用于 ACK 部分集群类型。新建议优先通过 `ecctl ack nodepool attach`（对应 `AttachInstancesToNodePool`）将实例加入指定节点池，节点池侧承载更完整的运行时配置、自愈和升级语义。`AttachInstances` 是异步操作，默认等待节点进入 Ready 状态并回读节点视图。

## 暂不进入主命令面的 API

K8s 节点的运维动作属于 Kubernetes 资源面，由 Agent 通过 `kubectl` 完成，不在 ACK 控制面 CLI 中重复实现：

- `kubectl cordon <node>` / `kubectl uncordon <node>`：将节点标记为不可调度 / 恢复调度。
- `kubectl drain <node>`：驱逐节点上的 Pod，常作为 `node delete` 的前置步骤。
- `kubectl taint nodes <node> <key>=<value>:<effect>` / `kubectl label node <node> <key>=<value>`：节点污点与标签变更。

节点上的 OS / kubelet / runtime 升级与异常修复由 [nodepool.md](nodepool.md) 的 `nodepool upgrade` 和 `nodepool repair` 承载，不在 `ack node` 重复定义。

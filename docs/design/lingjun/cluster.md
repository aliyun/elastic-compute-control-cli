# lingjun cluster

资源：灵骏集群

优先级：P0

本文件只描述 `ecctl lingjun cluster` 的 interface 级命令设计：每个操作命令对应哪些 eflo-controller API，不展开完整 flag、参数结构和输出结构；仅在多 API 分流时标明必要的特殊开关。

设计目标：覆盖灵骏集群的全生命周期（创建/扩缩容/删除/查询），所有操作必须通过 `ecctl lingjun cluster <action>` 调用。`update` 内承载集群扩容（`--extend`，对应 `ExtendCluster`）和缩容（`--shrink`，对应 `ShrinkCluster`）两种运维动作。`get` 支持通过 `--with-nodes` 和 `--with-hyper-nodes` 追加查询节点列表和超节点列表。灵骏集群没有基础属性 update 接口，cluster update 只承载扩缩容运维动作；集群基础属性变更（如名称）需在创建时确定。

特殊开关命名是 interface 级建议，用来说明何时触发额外 API；最终字段名和参数细节在资源 spec 中定义。

## `ecctl lingjun cluster create`

调用 API：

- [CreateCluster](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-controller-2022-12-15-createcluster)：创建灵骏集群。
- [DescribeCluster](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-controller-2022-12-15-describecluster)：轮询集群状态。

注意事项：`CreateCluster` 是异步创建接口，默认等待集群进入 ready 状态并回读集群视图；高级用户可 `--no-wait` 立即返回。
形态：`ecctl lingjun cluster create --name <name> --cluster-type <type> --node-groups @node-groups.json`

## `ecctl lingjun cluster update`

调用 API：

- 指定 `--extend` 时调用 [ExtendCluster](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-controller-2022-12-15-extendcluster)：集群扩容（scale out），添加节点到集群。
- 指定 `--shrink` 时调用 [ShrinkCluster](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-controller-2022-12-15-shrinkcluster)：集群缩容（scale in），从集群移除节点。
- [DescribeCluster](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-controller-2022-12-15-describecluster)：回读集群变更后的资源视图。

注意事项：灵骏集群的 `update` 专注于扩缩容场景，`--extend` 和 `--shrink` 二选一，不可同时指定。扩缩容是异步操作，默认等待集群恢复 ready 状态并回读集群视图。
形态：`ecctl lingjun cluster update <cluster-id> --extend <nodes>` 与 `ecctl lingjun cluster update <cluster-id> --shrink <nodes>`

## `ecctl lingjun cluster delete`

调用 API：

- [DeleteCluster](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-controller-2022-12-15-deletecluster)：删除灵骏集群。
- [DescribeCluster](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-controller-2022-12-15-describecluster)：确认集群删除完成。

注意事项：`DeleteCluster` 是异步操作，默认等待集群消失或进入删除终态；高级用户可 `--no-wait` 立即返回。
形态：`ecctl lingjun cluster delete <cluster-id>`

## `ecctl lingjun cluster get`

调用 API：

- 默认调用 [DescribeCluster](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-controller-2022-12-15-describecluster)：查询单个集群详情。
- 指定 `--with-nodes` 时调用 [ListClusterNodes](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-controller-2022-12-15-listclusternodes)：附带查询集群下的节点列表。
- 指定 `--with-hyper-nodes` 时调用 [ListClusterHyperNodes](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-controller-2022-12-15-listclusterhypernodes)：附带查询集群下的超节点列表。

注意事项：默认只取集群基础详情；节点列表和超节点列表通过特殊开关按需追加查询，避免 `get` 默认触发过多 API。
形态：`ecctl lingjun cluster get <cluster-id> [--with-nodes] [--with-hyper-nodes]`

## `ecctl lingjun cluster list`

调用 API：

- [ListClusters](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-controller-2022-12-15-listclusters)：查询灵骏集群列表。

分页约束：ListClusters 使用 NextToken/MaxResults 分页，默认 `--limit 100`，最大值以官方 API 文档为准。
形态：`ecctl lingjun cluster list [--filter ...] [--limit 100]`

## 暂不进入主命令面的 API

以下 API 已调研，但不作为 `ecctl lingjun cluster` 的 80% 主命令能力；需要时可先通过原始 OpenAPI 兜底。

- [DescribeTask](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-controller-2022-12-15-describetask)：异步任务状态查询为内部轮询实现，不独立暴露；需要时走 `ecctl aliyun eflo-controller DescribeTask`。
- [DescribeRegions](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-controller-2022-12-15-describeregions)：地域列表查询低频且通用，不挂在 cluster 子命令下。
- [DescribeZones](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-controller-2022-12-15-describezones)：可用区列表查询同上。
- [ChangeResourceGroup](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-controller-2022-12-15-changeresourcegroup)：资源组变更低频操作；需要时走原始 OpenAPI。
- [ApproveOperation](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-controller-2022-12-15-approveoperation)：运维审批为内部流程，不在 CLI 主路径。
- [ListUserClusterTypes](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-controller-2022-12-15-listuserclustertypes)：集群类型枚举为创建前辅助查询，不独立暴露。
- [ListMachineTypes](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-controller-2022-12-15-listmachinetypes)：机型列表为创建前辅助查询，不独立暴露。
- [ListImages](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-controller-2022-12-15-listimages)：镜像列表为创建前辅助查询，不独立暴露。
- [TagResources](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-controller-2022-12-15-tagresources)：标签变更走 `ecctl tag` 顶层资源；后续 cluster update 可考虑通过 `--tag` 差异计算后调用。
- [UntagResources](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-controller-2022-12-15-untagresources)：同上。
- [ListTagResources](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-controller-2022-12-15-listtagresources)：跨产品标签查询走 `ecctl tag resource list`。

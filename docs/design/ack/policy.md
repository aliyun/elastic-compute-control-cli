# ack policy

资源：策略治理

优先级：P2

本文件只描述 `ecctl ack policy` 的 interface 级命令设计：每个操作命令对应哪些 ACK API，不展开完整 flag、参数结构和输出结构；仅在多 API 分流时标明必要的特殊开关。

设计目标：覆盖 ACK 基于 OPA / Gatekeeper 的策略治理主线。策略分两层：

- **策略库（library）**：平台预置的策略规则元信息，跨集群只读，由 `policy list` / `policy get` 暴露，不提供创建/修改入口。平台目录视图通过 `list --catalog`（已是 list 的默认含义）与单条 `get --catalog` 表达，不独立设计 `policy catalog` 子命令。
- **策略实例（instance）**：在某个集群上部署的策略实例，是可写资源，统一收敛在 `instance` 子资源动作下，命名遵循 `policy instance <verb>` 子资源格式；动作集只用词表内 `create / update / delete / get / list`，运行态视图合入 `instance get`，不独立设计 `instance status`。
- **集群治理总览**：回答"某集群当前部署了哪些策略、违规情况如何"的视图与集群强绑定，统一合入 `cluster get --with-policy-governance`，不在 `policy` 资源下设计独立动作。

特殊开关命名是 interface 级建议，用来说明何时触发额外 API；最终字段名和参数细节在资源 spec 中定义。

## `ecctl ack policy get`

调用 API：

- [DescribePolicyDetails](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-describepolicydetails)：查询单条策略规则详情。

注意事项：`get` 返回策略库中的规则元信息（描述、参数 schema、严重等级等），与集群无关；要查询某集群上某条策略的部署状态，使用 `ecctl ack policy instance get`。

## `ecctl ack policy list`

调用 API：

- [DescribePolicies](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-describepolicies)：列出策略治理规则库。

注意事项：策略库是平台维度只读视图，不带集群过滤；集群维度的策略治理总览通过 `ecctl ack cluster get --with-policy-governance` 查询，集群已部署的策略实例清单通过 `ecctl ack policy instance list` 查询。

## `ecctl ack policy instance create`

调用 API：

- [DeployPolicyInstance](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-deploypolicyinstance)：在集群部署一个策略实例。
- [DescribePolicyInstancesStatus](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-describepolicyinstancesstatus)：回读策略实例的部署状态。

注意事项：`DeployPolicyInstance` 是异步部署接口，默认等待实例进入可观测状态并回读 status。

## `ecctl ack policy instance update`

调用 API：

- [ModifyPolicyInstance](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-modifypolicyinstance)：修改策略实例参数。
- [DescribePolicyInstancesStatus](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-describepolicyinstancesstatus)：回读修改后的策略实例状态。

注意事项：动词 `Modify` 归一为 `update`；参数变更（例如 enforcement 模式、parameters 字段）按期望值整体提交，由 spec 决定是否做差异计算。

## `ecctl ack policy instance delete`

调用 API：

- [DeletePolicyInstance](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-deletepolicyinstance)：删除策略实例。
- [DescribePolicyInstances](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-describepolicyinstances)：确认实例已从集群部署清单移除。

注意事项：删除是异步操作，默认等待策略实例不可见并回读 `instance list` 视图。

## `ecctl ack policy instance get`

调用 API：

- [DescribePolicyInstancesStatus](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-describepolicyinstancesstatus)：查询策略实例的部署详情与运行态（enforcement 模式、违规对象列表等）。

注意事项：`instance get` 默认返回部署 + 运行态合并视图（部署参数 + enforcement 模式 + 当前违规对象列表），不独立设计 `instance status`；这符合 cli-design-rules"开关/方向/状态/结果差异都用 flag，不拆 `*-status`"的规则。策略元信息查询走 `ecctl ack policy get`。

## `ecctl ack policy instance list`

调用 API：

- [DescribePolicyInstances](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-describepolicyinstances)：列出集群已部署策略实例。

注意事项：必须指定集群，是 `instance` 子资源动作的入口；与 `policy list` 区分——前者是"某集群上部署了哪些策略实例"，后者是"平台支持哪些策略规则"。

## 暂不进入主命令面的 API

策略治理范围内的 API 已全部进入主命令面，无暂缓项。其中：

- `DescribePolicyGovernanceInCluster` 已合入 `ack cluster get --with-policy-governance`，不在 `policy` 下独立设计 `governance` 动作。
- `DescribePolicyInstancesStatus` 同时承担 `instance get` 的主详情视图与 `instance create/update` 的回读。

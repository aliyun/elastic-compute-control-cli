# ack cluster

资源：集群

优先级：P0

别名：无（`cluster` 是 ACK 默认资源，通过 `ecctl ack <action>` 直接调用，不需要额外别名）

本文件只描述 `ecctl ack cluster` 的 interface 级命令设计：每个操作命令对应哪些 ACK API，不展开完整 flag、参数结构和输出结构；仅在多 API 分流时标明必要的特殊开关。

设计目标：cluster 是 ACK 的默认资源，支持 `ecctl ack create/update/delete/get/list/upgrade` 直接调用，覆盖集群本身的全生命周期（创建/修改/删除/查询/升级）。`update` 内承载基础属性变更、版本以外的形态变更（如基础版→Pro 版迁移，通过 `--to-edition` 触发 `MigrateCluster`），以及标签关系；`upgrade` 是 K8s 版本升级的独立运维动作（任务模型可暂停/恢复/取消）。`ecctl ack` 只覆盖阿里云控制面 API，不暴露 Pod、Deployment、Service、Ingress、ConfigMap 等 Kubernetes 原生资源面，由 `ack kubeconfig create` 引导到 `kubectl`。集群创建/删除/升级等异步操作默认等待目标状态并回读集群视图，高级用户可显式 `--no-wait` 拿到 `task_id` 后用 `ack task ...` 操作。

特殊开关命名是 interface 级建议，用来说明何时触发额外 API；最终字段名和参数细节在资源 spec 中定义。

## `ecctl ack cluster create`

调用 API：

- [CreateCluster](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-createcluster)：创建 ACK 集群。
- [DescribeClusterDetail](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-describeclusterdetail)：回读集群状态和资源视图。

注意事项：托管版、Serverless、Edge、注册集群通过请求体的 `type` 字段区分，统一使用 `CreateCluster`，不再独立设计 `cluster create-managed`、`cluster create-serverless` 等子命令。`CreateCluster` 是异步创建接口，默认等待集群进入 running 状态并回读集群视图；高级用户可 `--no-wait` 拿到 `task_id` 后用 `ack task ...` 跟踪。集群所属资源组在 `create` 时通过参数表达，不独立设计 `resource-group` 子命令。

## `ecctl ack cluster update`

调用 API：

- 修改集群名称、API Server SLB 公网开关、维护窗口、资源组归属等基础属性时调用 [ModifyCluster](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-modifycluster)：修改集群基础属性。
- 指定 `--to-edition` 形态迁移时调用 [MigrateCluster](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-migratecluster)：将集群迁移到目标形态（如基础版 → Pro 版）。
- 指定 `--tag-replace` 全量替换标签时调用 [ModifyClusterTags](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-modifyclustertags)：全量替换集群标签。
- 指定标签新增或修改时调用 [TagResources](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-tagresources)：为集群绑定标签。
- 指定标签移除时调用 [UntagResources](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-untagresources)：为集群解绑标签。
- [DescribeClusterDetail](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-describeclusterdetail)：回读集群变更后的资源视图。
- [ListTagResources](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-listtagresources)：回读集群标签。

注意事项：基础属性变更（名称、API Server SLB、维护窗口、资源组归属等）统一走 `ModifyCluster`，不独立设计 `rename`、`maintenance update`、`resource-group update` 子命令。集群形态变更（基础版迁移到 Pro 版等）是期望状态变更，通过 `--to-edition <pro|...>` 触发 `MigrateCluster`，不独立成 `cluster migrate` 子命令。`MigrateCluster` 是异步操作，返回 `task_id`，默认等待迁移完成并回读集群视图；高级用户可 `--no-wait` 拿到 `task_id` 后用 `ack task ...` 跟踪。标签按期望集合计算差异后分别调用 `TagResources` 和 `UntagResources`；当用户显式使用 `--tag-replace` 全量语义时改用 `ModifyClusterTags`，二者二选一。涉及异步变更时默认等待目标状态并回读集群视图；标签存在最终一致延迟时默认等待目标关系可见并回读。

## `ecctl ack cluster delete`

调用 API：

- [DeleteCluster](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-deletecluster)：删除集群。
- [DescribeClusterDetail](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-describeclusterdetail)：确认集群删除完成。

注意事项：`DeleteCluster` 支持是否清理集群关联的阿里云资源（VPC、SLB、NAT 等），通过请求体参数表达，不独立设计 `delete --purge` 之外的子命令。`DeleteCluster` 是异步操作，默认等待集群消失或进入删除终态；高级用户可 `--no-wait` 拿到 `task_id` 后用 `ack task ...` 跟踪。

## `ecctl ack cluster get`

调用 API：

- 默认调用 [DescribeClusterDetail](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-describeclusterdetail)：查询单个集群详情。
- 指定 `--with-resources` 时调用 [DescribeClusterResources](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-describeclusterresources)：附带查询集群关联的阿里云资源（VPC、vSwitch、SLB、安全组等）。
- 指定 `--with-tags` 时调用 [ListTagResources](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-listtagresources)：附带查询集群标签。
- 指定 `--with-policy-governance` 时调用 [DescribePolicyGovernanceInCluster](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-describepolicygovernanceincluster)：附带查询集群策略治理总览（已部署策略、违规计数）。

注意事项：默认只取集群基础详情；特殊开关用于按需追加查询，避免 `get` 默认触发过多 API。标签是集群关系视图，不在 `ack` 产品下单独设计 `tag list`；跨产品标签查询由 `ecctl tag resource list` 承载。`DescribeClusterResources` 通过 `get --with-resources` 表达，不独立设计 `cluster resources` 子命令；具体资源详情走 `ecctl vpc ...` 等其它产品命令。策略治理总览通过 `get --with-policy-governance` 合入集群视图，不独立设计 `policy governance` 子命令；策略实例的部署/详情走 `ack policy instance ...`。

## `ecctl ack cluster list`

调用 API：

- 默认调用 [DescribeClustersV1](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-describeclustersv1)：查询集群列表。
- 指定 `--cross-account` 时调用 [DescribeClustersForRegion](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-describeclustersforregion)：按地域聚合查询全部集群（含跨账号/特殊场景）。

注意事项：默认使用 `DescribeClustersV1` 覆盖大多数列表场景；按地域聚合的跨账号场景通过 `--cross-account` 切到 `DescribeClustersForRegion`，不独立设计 `cluster list-for-region` 子命令。

## `ecctl ack cluster upgrade`

调用 API：

- [UpgradeCluster](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-upgradecluster)：手动升级集群 Kubernetes 版本。
- [DescribeClusterDetail](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-describeclusterdetail)：回读集群升级后的版本和状态。

注意事项：`UpgradeCluster` 是异步操作，返回 `task_id`，默认等待升级任务完成并回读集群视图；高级用户可 `--no-wait` 拿到 `task_id` 后用 `ack task ...` 暂停/恢复/取消。`upgrade` 已登记在 [cli-design-rules.md](../cli-design-rules.md) Action 词表，专门用于有任务模型的版本升级，不收敛到 `update --target-version`。旧的升级状态查询/暂停/恢复/取消 API 已废弃，处理见最后的废弃/不推荐 API 章节。

## 暂不进入主命令面的 API

以下 API 已调研，但不作为 `ecctl ack cluster` 的 80% 主命令能力；其中部分会由其他资源命令承载，其余需要时可先通过原始 OpenAPI 兜底。

- [DescribeClusterLogs](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-describeclusterlogs)：集群控制面/组件日志检索语义偏排障且低频，主路径仍是 `kubectl logs` 与 SLS 日志查询；需要时走 `ecctl aliyun cs DescribeClusterLogs`。
- [DescribeUserQuota](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-describeuserquota)：用户级配额（集群/节点池/节点），与单个 cluster 实例无关，不挂在 cluster 子命令下；需要时走 `ecctl aliyun cs DescribeUserQuota`。
- [DescribeUserClusterNamespaces](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-describeuserclusternamespaces)：命名空间属于 Kubernetes 资源面，应该走 `kubectl get ns`，不在 ACK 控制面 CLI。
- [OpenAckService](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-openackservice)：一次性服务开通，引导式调用即可，不需要日常 CLI 入口。
- [UpdateKMSEncryption](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-updatekmsencryption)：Secret 落盘加密属于安全敏感场景，先用 `ecctl aliyun cs UpdateKMSEncryption` 兜底。
- [UpdateResourcesDeleteProtection](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-updateresourcesdeleteprotection)：删除保护属于命名空间/服务级 K8s 资源面属性，更适合走 `kubectl` 注解。
- [DescribeResourcesDeleteProtection](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-describeresourcesdeleteprotection)：同上，删除保护查询走 `kubectl`。
- [CreateAutoscalingConfig](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-createautoscalingconfig)：集群弹性伸缩配置参数复杂，先维持 console 创建，后续按需补 `cluster autoscaling-config update`。

## 废弃/不推荐 API

以下升级状态相关 API 已被通用 `task` 模型取代，迁移到 `ack task ...`，`ecctl ack cluster` 不接入：

- [GetUpgradeStatus](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-getupgradestatus)：使用 `ack task get`。
- [PauseClusterUpgrade](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-pauseclusterupgrade)：使用 `ack task pause`。
- [CancelClusterUpgrade](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-cancelclusterupgrade)：使用 `ack task cancel`。
- [ResumeUpgradeCluster](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-resumeupgradecluster)：使用 `ack task resume`。

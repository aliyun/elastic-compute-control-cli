# ack nodepool

资源：节点池

优先级：P0

别名：`np`

本文件只描述 `ecctl ack nodepool` 的 interface 级命令设计：每个操作命令对应哪些 ACK API，不展开完整 flag、参数结构和输出结构；仅在多 API 分流时标明必要的特殊开关。

设计目标：覆盖节点池全生命周期、扩缩容、升级、修复、CVE 修复，以及 ECS 实例的 attach / detach。扩缩容是期望状态变更，收敛到 `update --desired-size`；K8s 版本/镜像/runtime 升级保留独立 `upgrade`（任务模型可暂停/恢复/取消）；节点修复与 CVE 修复都属于节点级修复维度，收敛到 `repair`，CVE 修复用 `repair --vulnerabilities` 分流。已有 ECS 加入 ACK 统一收敛在节点池侧（`attach`），不在 `ecctl ack node` 重复 attach 入口；attach 脚本只在 `attach --print-script-only` 模式下输出，不独立成命令。漏洞扫描与详情查询走 `ecctl ack vuls`。

特殊开关命名是 interface 级建议，用来说明何时触发额外 API；最终字段名和参数细节在资源 spec 中定义。

## `ecctl ack nodepool create`

调用 API：

- [CreateClusterNodePool](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-createclusternodepool)：创建节点池，含规格、镜像、计费、伸缩等配置。
- [DescribeClusterNodePoolDetail](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-describeclusternodepooldetail)：回读节点池视图。

注意事项：`CreateClusterNodePool` 是异步创建接口，默认等待节点池就绪并回读节点池视图。

## `ecctl ack nodepool update`

调用 API：

- 默认调用 [ModifyClusterNodePool](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-modifyclusternodepool)：修改节点池配置（伸缩范围、节点池自身标签、taint、kubelet 配置项等）。
- 指定 `--desired-size` 时调用 [ScaleClusterNodePool](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-scaleclusternodepool)：把节点池扩缩容到目标节点数。
- 指定 `--with-node-config` 时调用 [ModifyNodePoolNodeConfig](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-modifynodepoolnodeconfig)：修改节点池下节点的 kubelet / runtime / 容器引擎等节点级配置。
- [DescribeClusterNodePoolDetail](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-describeclusternodepooldetail)：回读节点池视图。

注意事项：节点池本身配置变更、节点数量期望状态、节点级运行时配置变更都归入 `update`，不再独立设计 `nodepool scale`、`node-config update` 子命令。`ModifyClusterNodePool`、`ScaleClusterNodePool` 和 `ModifyNodePoolNodeConfig` 都返回异步 `task_id`，默认等待任务成功并回读节点池视图。ACK 的 `TagResources` / `UntagResources` 目前只接受 `resource_type=CLUSTER`，因此 nodepool update 不暴露 `--tag` / `--untag`；节点池内部标签如需修改，应通过 `--config` 交给 `ModifyClusterNodePool`。

## `ecctl ack nodepool delete`

调用 API：

- [DeleteClusterNodepool](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-deleteclusternodepool)：删除节点池。

注意事项：删除是异步操作，默认等待节点池消失。

## `ecctl ack nodepool get`

调用 API：

- 默认调用 [DescribeClusterNodePoolDetail](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-describeclusternodepooldetail)：查询节点池详情。
- 指定 `--with-vuls` 时调用 [DescribeNodePoolVuls](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-describenodepoolvuls)：附带节点池漏洞详情。

注意事项：默认只取节点池基础详情；漏洞详情默认不拉取，避免 `get` 默认触发过多 API。需要批量看漏洞时使用 `ecctl ack vuls list --nodepool` 而非 `get --with-vuls`。

## `ecctl ack nodepool list`

调用 API：

- [DescribeClusterNodePools](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-describeclusternodepools)：查询集群下所有节点池。

## `ecctl ack nodepool upgrade`

调用 API：

- [UpgradeClusterNodepool](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-upgradeclusternodepool)：升级节点池 kubelet / OS / runtime 版本。
- [DescribeClusterNodePoolDetail](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-describeclusternodepooldetail)：回读节点池视图。

注意事项：升级是异步操作，返回 `task_id`，默认等待升级任务完成并回读节点池视图；高级用户可 `--no-wait` 拿到 `task_id` 后用 `ack task ...` 暂停/恢复/取消。`upgrade` 已登记在 [cli-design-rules.md](../cli-design-rules.md) Action 词表，专门用于有任务模型的版本升级。

## `ecctl ack nodepool repair`

调用 API：

- 默认调用 [RepairClusterNodePool](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-repairclusternodepool)：修复节点池中的异常节点（重置 / 重装）。
- 指定 `--vulnerabilities` 时调用 [FixNodePoolVuls](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-fixnodepoolvuls)：按漏洞 ID 列表修复节点池节点上的 CVE 漏洞。

注意事项：节点修复与 CVE 修复都属于节点级修复维度，统一收敛到 `repair`，CVE 修复用 `--vulnerabilities` flag 分流，不独立设计 `nodepool fix-vuls`。两个 API 都是异步操作，默认等待修复任务完成。漏洞扫描和详情查询归入 `ecctl ack vuls`。

## `ecctl ack nodepool attach`

调用 API：

- 默认调用 [AttachInstancesToNodePool](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-attachinstancestonodepool)：把已有 ECS 实例加入指定节点池。
- 指定 `--print-script-only` 时改为调用 [DescribeClusterAttachScripts](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-describeclusterattachscripts)：仅打印 attach 脚本，用于自助加入场景。

注意事项：已有 ECS 加入 ACK 的入口统一在节点池侧，不在 `ecctl ack node` 重复 attach 命令。`AttachInstancesToNodePool` 是异步操作，默认等待节点 Ready 并回读节点池视图。`--print-script-only` 模式下不实际触发 attach，只输出脚本，不需要等待。

## `ecctl ack nodepool detach`

调用 API：

- [RemoveNodePoolNodes](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-removenodepoolnodes)：从节点池移除指定节点（不删除底层 ECS）。

注意事项：仅解除节点与节点池的关联，不释放 ECS 实例；如需同时删除 ECS，请走 `ecctl ack node delete` 或 `ecctl ecs instance delete`。`RemoveNodePoolNodes` 是异步操作，默认等待移除完成。

## 暂不进入主命令面的 API

以下 API 已调研，但不作为 `ecctl ack nodepool` 的 80% 主命令能力；需要时可先通过原始 OpenAPI 兜底。

- [SyncClusterNodePool](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-syncclusternodepool)：节点池元数据与真实状态的同步是异常态修复手段，正常运维不应使用；需要时走 `ecctl aliyun cs SyncClusterNodePool`。
- [InstallNodePoolComponents](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-installnodepoolcomponents)：节点级组件细粒度安装，已被 `nodepool repair` / `nodepool upgrade` 覆盖大部分场景；需要时走 `ecctl aliyun cs InstallNodePoolComponents`。
- [UpdateNodePoolComponent](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-updatenodepoolcomponent)：节点级组件细粒度更新，同上；需要时走 `ecctl aliyun cs UpdateNodePoolComponent`。
- [CreateAutoscalingConfig](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-createautoscalingconfig)：集群弹性伸缩配置参数复杂，先维持 console 创建，后续按需补 `ack cluster autoscaling-config update`。

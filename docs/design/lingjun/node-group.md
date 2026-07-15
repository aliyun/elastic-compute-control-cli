# lingjun node-group

资源：灵骏节点组

优先级：P1

别名：`ng`

本文件只描述 `ecctl lingjun node-group` 的 interface 级命令设计：每个操作命令对应哪些 eflo-controller API，不展开完整 flag、参数结构和输出结构；仅在多 API 分流时标明必要的特殊开关。

设计目标：覆盖灵骏节点组的全生命周期（创建/修改/删除/列表查询）。节点组是集群内节点的逻辑分组，用于统一管理一组同质节点的配置和调度策略。单个节点组的详情通过 `ecctl lingjun cluster get --with-nodes` 间接查看，不单独设计 `get` 动作。

特殊开关命名是 interface 级建议，用来说明何时触发额外 API；最终字段名和参数细节在资源 spec 中定义。

## `ecctl lingjun node-group create`

调用 API：

- [CreateNodeGroup](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-controller-2022-12-15-createnodegroup)：创建节点组。

注意事项：`CreateNodeGroup` 是同步接口，创建成功后直接返回节点组 ID。

形态：`ecctl lingjun node-group create --cluster <c-xxx> --name <name> ...`

## `ecctl lingjun node-group update`

调用 API：

- [UpdateNodeGroup](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-controller-2022-12-15-updatenodegroup)：更新节点组配置。
- [DescribeNodeGroup](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-controller-2022-12-15-describenodegroup)：回读节点组变更后的资源视图。

注意事项：更新后默认回读节点组视图，确认变更已生效。

形态：`ecctl lingjun node-group update <ng-id> [--name ...] ...`

## `ecctl lingjun node-group delete`

调用 API：

- [DeleteNodeGroup](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-controller-2022-12-15-deletenodegroup)：删除节点组。

注意事项：删除节点组不会删除组内的节点，节点会变为未分组状态。

形态：`ecctl lingjun node-group delete <ng-id>`

## `ecctl lingjun node-group list`

调用 API：

- [ListNodeGroups](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-controller-2022-12-15-listnodegroups)：按集群查询节点组列表。

注意事项：`--filter cluster=` 必填，指定集群，返回该集群下所有节点组。

分页约束：`ListNodeGroups` 使用 `NextToken`/`MaxResults` 分页，默认 `--limit 100`，最大值以官方 API 文档为准。

形态：`ecctl lingjun node-group list --filter cluster=<c-xxx>`

## 暂不进入主命令面的 API

以下 API 已调研，但不作为 `ecctl lingjun node-group` 的 80% 主命令能力；需要时可先通过原始 OpenAPI 兜底。

- [ChangeNodeGroup](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-controller-2022-12-15-changenodegroup)：在节点组之间移动节点，属于低频运维操作；需要时走 `ecctl aliyun eflo-controller ChangeNodeGroup`。

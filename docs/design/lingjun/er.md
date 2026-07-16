# lingjun er

资源：灵骏HUB (Enterprise Router)

优先级：P2

本文件只描述 `ecctl lingjun er` 的 interface 级命令设计：每个操作命令对应哪些 eflo API，不展开完整 flag、参数结构和输出结构；仅在多 API 分流时标明必要的特殊开关。

设计目标：覆盖灵骏 HUB（Enterprise Router）的全生命周期（创建/修改/删除/查询）。ER 是灵骏网络的核心路由设备，连接多个 VPD 和 VCC，实现跨网段的流量转发和路由策略管理。`update` 内承载基础属性变更、Attachment（连接关系）的增删改，以及 RouteMap（路由策略）的增删改；`get` 支持通过 `--with-attachments`、`--with-route-maps`、`--with-routes` 追加查询关联信息。

特殊开关命名是 interface 级建议，用来说明何时触发额外 API；最终字段名和参数细节在资源 spec 中定义。

## `ecctl lingjun er create`

调用 API：

- [CreateEr](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-2022-05-30-createer)：创建 Enterprise Router。
- [GetEr](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-2022-05-30-geter)：回读 ER 视图。

注意事项：创建成功后回读 ER 视图确认状态。

形态：`ecctl lingjun er create --name <name> ...`

## `ecctl lingjun er update`

调用 API：

- 默认调用 [UpdateEr](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-2022-05-30-updater)：修改 ER 基础属性。
- 指定 `--attachment +...` 时调用 [CreateErAttachment](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-2022-05-30-createerattachment)：为 ER 添加连接关系（绑定 VPD 或 VCC）。
- 指定 `--attachment -<att-id>` 时调用 [DeleteErAttachment](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-2022-05-30-deleteerattachment)：删除 ER 的连接关系。
- 修改已有 Attachment 属性时调用 [UpdateErAttachment](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-2022-05-30-updateerattachment)：更新 ER 连接关系属性。
- 指定 `--route-map +...` 时调用 [CreateErRouteMap](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-2022-05-30-createerroutemap)：为 ER 添加路由策略。
- 指定 `--route-map -<rm-id>` 时调用 [DeleteErRouteMap](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-2022-05-30-deleteerroutemap)：删除 ER 的路由策略。
- 修改已有 RouteMap 属性时调用 [UpdateErRouteMap](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-2022-05-30-updateerroutemap)：更新 ER 路由策略属性。
- [GetEr](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-2022-05-30-geter)：回读 ER 变更后的资源视图。

注意事项：ER 的 `update` 是一个复合操作，基础属性变更、Attachment 管理和 RouteMap 管理可在同一次调用中组合使用。变更后默认回读 ER 视图，确认变更已生效。

形态：`ecctl lingjun er update <er-id> [--attachment +...] [--attachment -<att-id>] [--route-map +...] [--route-map -<rm-id>]`

## `ecctl lingjun er delete`

调用 API：

- [DeleteEr](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-2022-05-30-deleteer)：删除 Enterprise Router。

注意事项：删除前需确保 ER 下无 Attachment 和 RouteMap，否则删除会失败。

形态：`ecctl lingjun er delete <er-id>`

## `ecctl lingjun er get`

调用 API：

- 默认调用 [GetEr](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-2022-05-30-geter)：查询单个 ER 详情。
- 指定 `--with-attachments` 时调用 [ListErAttachments](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-2022-05-30-listerattachments)：附带查询 ER 的连接关系列表。
- 指定 `--with-route-maps` 时调用 [ListErRouteMaps](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-2022-05-30-listerroutemaps)：附带查询 ER 的路由策略列表。
- 指定 `--with-routes` 时调用 [ListErRouteEntries](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-2022-05-30-listerrouteentries)：附带查询 ER 的路由条目列表。

注意事项：默认只取 ER 基础详情；连接关系、路由策略和路由条目通过特殊开关按需追加查询，避免 `get` 默认触发过多 API。

形态：`ecctl lingjun er get <er-id> [--with-attachments] [--with-route-maps] [--with-routes]`

## `ecctl lingjun er list`

调用 API：

- [ListErs](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-2022-05-30-listers)：查询 Enterprise Router 列表。

分页约束：ListErs 使用 PageNumber/PageSize 分页，默认 `--limit 100`，最大值以官方 API 文档为准。

形态：`ecctl lingjun er list [--filter ...] [--limit 100]`

## 暂不进入主命令面的 API

以下 API 已调研，但不作为 `ecctl lingjun er` 的 80% 主命令能力；需要时可先通过原始 OpenAPI 兜底。

- [GetErAttachment](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-2022-05-30-geterattachment)：单个 Attachment 详情已通过 `get --with-attachments` 列表覆盖；需要时走原始 OpenAPI。
- [GetErRouteMap](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-2022-05-30-geterroutemap)：单个 RouteMap 详情已通过 `get --with-route-maps` 列表覆盖；需要时走原始 OpenAPI。
- [GetErRouteEntry](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-2022-05-30-geterrouteentry)：单条路由详情已通过 `get --with-routes` 列表覆盖；需要时走原始 OpenAPI。
- [ListInstancesByNcd](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-2022-05-30-listinstancesbyncd) / [QueryInstanceNcd](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-2022-05-30-queryinstancencd) / [GetFabricTopology](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-2022-05-30-getfabrictopology)：NCD（Network Communication Distance）相关 API 为网络拓扑分析场景，不在 CLI 主路径。
- [GetNodeInfoForPod](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-2022-05-30-getnodeinfoforpod) / [ListNodeInfosForPod](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-2022-05-30-listnodeinfosforpod)：Pod 级节点信息查询为容器调度场景内部接口，不独立暴露。
- [GetDestinationCidrBlock](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-2022-05-30-getdestinationcidrblock)：目标 CIDR 查询为路由计算辅助，不独立暴露。
- [RefundVcc](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-2022-05-30-refundvcc)：VCC 退款为商务流程，不在 CLI 主路径。

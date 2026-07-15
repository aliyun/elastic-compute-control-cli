# lingjun vpd

资源：灵骏网段

优先级：P1

本文件只描述 `ecctl lingjun vpd` 的 interface 级命令设计：每个操作命令对应哪些 eflo API，不展开完整 flag、参数结构和输出结构；仅在多 API 分流时标明必要的特殊开关。

设计目标：覆盖灵骏网段（VPD）的全生命周期（创建/修改/删除/查询）。VPD 是灵骏网络的基础资源，类似于 VPC 的概念，为灵骏集群提供隔离的网络空间。`update` 内承载基础属性变更和 CIDR 地址段的关联/解关联操作；`get` 支持通过 `--with-routes` 和 `--with-grants` 追加查询路由条目和授权规则。

特殊开关命名是 interface 级建议，用来说明何时触发额外 API；最终字段名和参数细节在资源 spec 中定义。

## `ecctl lingjun vpd create`

调用 API：

- [CreateVpd](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-2022-05-30-createvpd)：创建灵骏网段。
- [GetVpd](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-2022-05-30-getvpd)：轮询网段状态。

注意事项：`CreateVpd` 是异步创建接口，默认等待网段进入可用状态并回读网段视图。

形态：`ecctl lingjun vpd create --name <name> --cidr <cidr>`

## `ecctl lingjun vpd update`

调用 API：

- 默认调用 [UpdateVpd](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-2022-05-30-updatevpd)：修改网段基础属性。
- 指定 `--cidr +<cidr>` 时调用 [AssociateVpdCidrBlock](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-2022-05-30-associatevpdcidrblock)：为网段关联新的 CIDR 地址段。
- 指定 `--cidr -<cidr>` 时调用 [UnAssociateVpdCidrBlock](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-2022-05-30-unassociatevpdcidrblock)：解除网段已关联的 CIDR 地址段。
- [GetVpd](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-2022-05-30-getvpd)：回读网段变更后的资源视图。

注意事项：基础属性变更和 CIDR 关联/解关联可在同一次 `update` 调用中组合使用。变更后默认回读网段视图，确认变更已生效。

形态：`ecctl lingjun vpd update <vpd-id> [--name ...] [--cidr +<cidr>] [--cidr -<cidr>]`

## `ecctl lingjun vpd delete`

调用 API：

- [DeleteVpd](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-2022-05-30-deletevpd)：删除灵骏网段。

注意事项：删除前需确保网段下无子网和关联资源，否则删除会失败。

形态：`ecctl lingjun vpd delete <vpd-id>`

## `ecctl lingjun vpd get`

调用 API：

- 默认调用 [GetVpd](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-2022-05-30-getvpd)：查询单个网段详情。
- 指定 `--with-routes` 时调用 [ListVpdRouteEntries](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-2022-05-30-listvpdrouteentries)：附带查询网段路由条目。
- 指定 `--with-grants` 时调用 [ListVpdGrantRules](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-2022-05-30-listvpdgrantrules)：附带查询网段授权规则。

注意事项：默认只取网段基础详情；路由条目和授权规则通过特殊开关按需追加查询，避免 `get` 默认触发过多 API。

形态：`ecctl lingjun vpd get <vpd-id> [--with-routes] [--with-grants]`

## `ecctl lingjun vpd list`

调用 API：

- [ListVpds](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-2022-05-30-listvpds)：查询灵骏网段列表。

分页约束：ListVpds 使用 PageNumber/PageSize 分页，默认 `--limit 100`，最大值以官方 API 文档为准。

形态：`ecctl lingjun vpd list [--filter ...] [--limit 100]`

## 暂不进入主命令面的 API

以下 API 已调研，但不作为 `ecctl lingjun vpd` 的 80% 主命令能力；需要时可先通过原始 OpenAPI 兜底。

- [CreateVpdGrantRule](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-2022-05-30-createvpdgrantrule) / [DeleteVpdGrantRule](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-2022-05-30-deletevpdgrantrule) / [GetVpdGrantRule](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-2022-05-30-getvpdgrantrule)：授权规则的增删查已通过 `get --with-grants` 和 `update` 组合覆盖主路径；单独的授权规则管理走原始 OpenAPI。
- [GetVpdRouteEntry](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-2022-05-30-getvpdrouteentry)：单条路由详情已通过 `get --with-routes` 列表覆盖；需要时走原始 OpenAPI。

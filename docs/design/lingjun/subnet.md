# lingjun subnet

资源：灵骏子网

优先级：P1

本文件只描述 `ecctl lingjun subnet` 的 interface 级命令设计：每个操作命令对应哪些 eflo API，不展开完整 flag、参数结构和输出结构；仅在多 API 分流时标明必要的特殊开关。

设计目标：覆盖灵骏子网的全生命周期（创建/修改/删除/查询）。子网是 VPD 下的网络细分，为灵骏集群中的节点和弹性网卡提供 IP 地址分配范围。每个子网归属于一个 VPD，创建时需指定所属 VPD 和 CIDR 地址段。

特殊开关命名是 interface 级建议，用来说明何时触发额外 API；最终字段名和参数细节在资源 spec 中定义。

## `ecctl lingjun subnet create`

调用 API：

- [CreateSubnet](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-2022-05-30-createsubnet)：创建子网。
- [GetSubnet](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-2022-05-30-getsubnet)：回读子网视图。

注意事项：创建时需指定所属 VPD ID、可用区和 CIDR 地址段，创建成功后回读子网视图确认状态。

形态：`ecctl lingjun subnet create --vpd <vpd-id> --zone <zone-id> --cidr <cidr> --name <name>`

## `ecctl lingjun subnet update`

调用 API：

- [UpdateSubnet](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-2022-05-30-updatesubnet)：修改子网属性。
- [GetSubnet](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-2022-05-30-getsubnet)：回读子网变更后的资源视图。

注意事项：更新后默认回读子网视图，确认变更已生效。`--vpd` 和 `--zone` 为 API 必传的资源定位参数。

形态：`ecctl lingjun subnet update <subnet-id> --vpd <vpd-id> --zone <zone-id> [--name ...]`

## `ecctl lingjun subnet delete`

调用 API：

- [DeleteSubnet](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-2022-05-30-deletesubnet)：删除子网。

注意事项：删除前需确保子网下无关联的弹性网卡等资源，否则删除会失败。`--vpd` 和 `--zone` 为 API 必传的资源定位参数。

形态：`ecctl lingjun subnet delete <subnet-id> --vpd <vpd-id> --zone <zone-id>`

## `ecctl lingjun subnet get`

调用 API：

- [GetSubnet](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-2022-05-30-getsubnet)：查询单个子网详情。

形态：`ecctl lingjun subnet get <subnet-id>`

## `ecctl lingjun subnet list`

调用 API：

- [ListSubnets](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-2022-05-30-listsubnets)：查询子网列表。

注意事项：支持按 VPD ID 过滤，查询指定网段下的所有子网。

分页约束：ListSubnets 使用 PageNumber/PageSize 分页，默认 `--limit 100`，最大值以官方 API 文档为准。

形态：`ecctl lingjun subnet list --filter vpd=<vpd-id> [--limit 100]`

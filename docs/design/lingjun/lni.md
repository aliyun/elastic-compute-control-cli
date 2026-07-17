# lingjun lni

资源：灵骏物理网卡

优先级：P1

本文件描述 `ecctl lingjun lni` 的 interface 级命令设计：每个操作命令对应哪些 eflo API，并记录影响 API 选择的关键 flag 形态。

设计目标：覆盖灵骏节点物理网卡（LNI，资源 ID `lni-xxx`）的查询和辅助 IP 管理。LNI 是节点出厂自带的物理网卡（NcType 包括 CUSTOM_LNI / CUSTOM_LNI_INTEGRATION / GPU / CPU / DEFAULT / ELASTIC_6.2），与节点强绑定，**用户不能创建或删除**，因此本资源不提供 create / delete 命令。LENI（用户创建/绑定的弹性网卡，ID `leni-xxx`）见 [eni.md](eni.md)。

## `ecctl lingjun lni update`

调用 API：

- 指定 `--ip +<ip>` 时调用 [AssignPrivateIpAddress](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-2022-05-30-assignprivateipaddress)：为 LNI 分配辅助私有 IP 地址。
- 指定 `--ip -<ip>` 时调用 [UnAssignPrivateIpAddress](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-2022-05-30-unassignprivateipaddress)：回收 LNI 辅助私有 IP 地址。
- [GetNetworkInterface](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-2022-05-30-getnetworkinterface)：回读 LNI 变更后的资源视图。

注意事项：`--ip +<ip>` 与 `--ip -<ip>` 可同时指定，CLI 内部先分配再回收；两者均可重复传入或以逗号分隔批量操作。辅助 IP 操作是异步动作，默认等待 IP 进入终态并回读网卡视图。LNI 自身基础属性（如名称、绑定关系）由节点托管，不在 update 范围内。
形态：`ecctl lingjun lni update <lni-id> [--ip +<ip>] [--ip -<ip>]`

## `ecctl lingjun lni get`

调用 API：

- [GetNetworkInterface](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-2022-05-30-getnetworkinterface)：查询单张 LNI 详情。
- 指定 `--with-ips` 时调用 [ListLniPrivateIpAddress](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-2022-05-30-listlniprivateipaddress)：附带查询该 LNI 的辅助私有 IP 列表。

注意事项：默认只取网卡基础详情；辅助 IP 列表通过 `--with-ips` 按需追加，避免 `get` 默认触发过多 API。
形态：`ecctl lingjun lni get <lni-id> [--with-ips]`

## `ecctl lingjun lni list`

调用 API：

- [ListNetworkInterfaces](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-2022-05-30-listnetworkinterfaces)：查询灵骏物理网卡列表。

注意事项：支持 `--node <node-id>` 按节点过滤；其它过滤项（如 NcType、ZoneId、VpdId）通过通用 `--filter` 透传。
分页约束：ListNetworkInterfaces 使用 PageNumber/PageSize 分页（如官方为 NextToken/MaxResults 请按官方描述），默认 `--limit 100`，最大值以官方 API 文档为准。
形态：`ecctl lingjun lni list [--node <node-id>] [--filter ...] [--limit 100]`

## 暂不进入主命令面的 API

以下 API 已调研，但不作为 `ecctl lingjun lni` 的 80% 主命令能力；需要时可先通过原始 OpenAPI 兜底。

- [GetLniPrivateIpAddress](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-2022-05-30-getlniprivateipaddress)：单条辅助 IP 详情已通过 `get --with-ips` 与 list 覆盖；需要单点查询时走 `ecctl aliyun eflo GetLniPrivateIpAddress`。

LNI 由灵骏节点出厂自带，没有 create / delete 命令；其生命周期跟随节点的上下架，不可由用户独立创建或删除。

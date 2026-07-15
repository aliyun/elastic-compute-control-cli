# lingjun eni

资源：灵骏弹性网卡

优先级：P2

本文件只描述 `ecctl lingjun eni` 的 interface 级命令设计：每个操作命令对应哪些 eflo API，不展开完整 flag、参数结构和输出结构；仅在多 API 分流时标明必要的特殊开关。

设计目标：覆盖灵骏弹性网卡（ENI）的全生命周期（创建/修改/删除/查询/绑定/解绑）。弹性网卡是灵骏网络中的虚拟网络接口，可以绑定到 NC（Node Controller）上为节点提供网络能力。`update` 内承载基础属性变更和私有 IP 的分配/释放操作；`attach` 和 `detach` 提供网卡与 NC 的绑定/解绑能力。本资源覆盖的是用户创建的弹性网卡（LENI，资源 ID `leni-xxx`），节点物理网卡（LNI，资源 ID `lni-xxx`）见 [lni.md](lni.md)。

特殊开关命名是 interface 级建议，用来说明何时触发额外 API；最终字段名和参数细节在资源 spec 中定义。

## `ecctl lingjun eni create`

调用 API：

- [CreateElasticNetworkInterface](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-2022-05-30-createelasticnetworkinterface)：创建弹性网卡。
- [GetElasticNetworkInterface](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-2022-05-30-getelasticnetworkinterface)：回读弹性网卡视图。

注意事项：创建时需指定所属子网，创建成功后回读网卡视图确认状态。

形态：`ecctl lingjun eni create --subnet <subnet-id> --vpd <vpd-id> --zone <zone-id> [--security-group <sg-id>] ...`

## `ecctl lingjun eni update`

调用 API：

- 默认调用 [UpdateElasticNetworkInterface](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-2022-05-30-updateelasticnetworkinterface)：修改弹性网卡基础属性。
- 指定 `--ip +<ip>` 时调用 [AssignLeniPrivateIpAddress](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-2022-05-30-assignleniprivateipaddress)：为弹性网卡分配新的私有 IP 地址。
- 指定 `--ip -<ip>` 时调用 [UnassignLeniPrivateIpAddress](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-2022-05-30-unassignleniprivateipaddress)：释放弹性网卡上的私有 IP 地址。
- [GetElasticNetworkInterface](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-2022-05-30-getelasticnetworkinterface)：回读弹性网卡变更后的资源视图。

注意事项：基础属性变更和私有 IP 管理可在同一次 `update` 调用中组合使用。变更后默认回读网卡视图，确认变更已生效。

形态：`ecctl lingjun eni update <leni-id> [--ip +<ip>] [--ip -<ip>]`

## `ecctl lingjun eni delete`

调用 API：

- [DeleteElasticNetworkInterface](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-2022-05-30-deleteelasticnetworkinterface)：删除弹性网卡。

注意事项：删除前需确保网卡已从 NC 解绑（`detach`），否则删除会失败。

形态：`ecctl lingjun eni delete <leni-id>`

## `ecctl lingjun eni get`

调用 API：

- 默认调用 [GetElasticNetworkInterface](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-2022-05-30-getelasticnetworkinterface)：查询单个弹性网卡详情。
- 指定 `--with-ips` 时调用 [ListLeniPrivateIpAddresses](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-2022-05-30-listleniprivateipaddresses)：附带查询网卡上的私有 IP 地址列表。

注意事项：默认只取网卡基础详情；私有 IP 列表通过特殊开关按需追加查询，避免 `get` 默认触发过多 API。

形态：`ecctl lingjun eni get <leni-id> [--with-ips]`

## `ecctl lingjun eni list`

调用 API：

- [ListElasticNetworkInterfaces](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-2022-05-30-listelasticnetworkinterfaces)：查询弹性网卡列表。

分页约束：ListElasticNetworkInterfaces 使用 PageNumber/PageSize 分页，默认 `--limit 100`，最大值以官方 API 文档为准。

形态：`ecctl lingjun eni list [--filter vpd=<vpd-id>] [--limit 100]`

## `ecctl lingjun eni attach`

调用 API：

- [AttachElasticNetworkInterface](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-2022-05-30-attachelasticnetworkinterface)：将弹性网卡绑定到 NC。

注意事项：绑定后网卡在目标 NC 上生效，为其提供网络连接能力。

形态：`ecctl lingjun eni attach <leni-id> --node <node-id>`

## `ecctl lingjun eni detach`

调用 API：

- [DetachElasticNetworkInterface](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-2022-05-30-detachelasticnetworkinterface)：将弹性网卡从 NC 解绑。

注意事项：解绑后网卡进入可用状态，可以重新绑定到其他 NC 或删除。

形态：`ecctl lingjun eni detach <leni-id> --node <node-id>`

## 暂不进入主命令面的 API

以下 API 已调研，但不作为 `ecctl lingjun eni` 的 80% 主命令能力；需要时可先通过原始 OpenAPI 兜底。

- [GetLeniPrivateIpAddress](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-2022-05-30-getleniprivateipaddress)：单个私有 IP 详情已通过 `get --with-ips` 列表覆盖；需要时走原始 OpenAPI。
- [UpdateLeniPrivateIpAddress](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-2022-05-30-updateleniprivateipaddress)：私有 IP 属性修改为极低频操作；需要时走原始 OpenAPI。

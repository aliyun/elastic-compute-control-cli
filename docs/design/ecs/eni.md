# ecs eni

资源：弹性网卡

优先级：P1

本文件描述 `ecctl ecs eni` 的 interface 级命令设计：每个操作命令对应哪些 ECS API，并记录影响 API 选择的关键 flag 形态。

## `ecctl ecs eni create`

调用 API：

- [CreateNetworkInterface](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-createnetworkinterface)：创建弹性网卡。
- [DescribeNetworkInterfaceAttribute](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describenetworkinterfaceattribute)：回读弹性网卡状态和资源视图。

注意事项：创建是异步操作时，默认等待弹性网卡进入可用状态并回读网卡视图。

## `ecctl ecs eni update`

调用 API：

- [ModifyNetworkInterfaceAttribute](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-modifynetworkinterfaceattribute)：修改弹性网卡属性。
- [AssignPrivateIpAddresses](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-assignprivateipaddresses)：为弹性网卡分配辅助私有 IP 地址。
- [UnassignPrivateIpAddresses](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-unassignprivateipaddresses)：从弹性网卡删除辅助私有 IP 地址。
- [AssignIpv6Addresses](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-assignipv6addresses)：为弹性网卡分配 IPv6 地址。
- [UnassignIpv6Addresses](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-unassignipv6addresses)：回收弹性网卡 IPv6 地址。
- [EnableNetworkInterfaceQoS](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-enablenetworkinterfaceqos)：启用或修改弹性网卡 QoS 限速设置。
- [DisableNetworkInterfaceQoS](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-disablenetworkinterfaceqos)：禁用弹性网卡 QoS 限速设置。
- [DescribeNetworkInterfaceAttribute](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describenetworkinterfaceattribute)：回读弹性网卡变更后的资源视图。

注意事项：地址和前缀增删归入中性参数，通过 `+value|-value` 前缀选择 API：`--private-ip +10.0.0.10` 分配，`--private-ip -10.0.0.10` 回收；`--ipv4-prefix +10.0.0.0/28` / `--ipv4-prefix -10.0.0.0/28`、`--ipv6-address +2408::1` / `--ipv6-address -2408::1`、`--ipv6-prefix +2408::/64` / `--ipv6-prefix -2408::/64` 同理。help 和 schema 中声明输入形态为 `+value|-value`。自动分配数量使用中性 count 参数，例如 `--private-ip-count`、`--ipv4-prefix-count`、`--ipv6-address-count`、`--ipv6-prefix-count`，只触发分配 API。QoS 使用 `--qos status=enable|disable,...`，`status=enable` 调用 `EnableNetworkInterfaceQoS`，`status=disable` 调用 `DisableNetworkInterfaceQoS`。不设计 `--assign-*`、`--unassign-*`、`--enable-qos`、`--disable-qos`、`--attach-instance-id`、`--detach-instance-id` 这类动作型 update flag。地址分配回收或 QoS 变更后默认回读网卡视图。

## `ecctl ecs eni delete`

调用 API：

- [DeleteNetworkInterface](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-deletenetworkinterface)：删除弹性网卡（ENI）。
- [DescribeNetworkInterfaces](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describenetworkinterfaces)：确认弹性网卡删除完成。

注意事项：删除是异步操作时，默认等待弹性网卡不可见或进入删除终态。

## `ecctl ecs eni get`

调用 API：

- [DescribeNetworkInterfaceAttribute](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describenetworkinterfaceattribute)：查询网卡属性。
- [DescribeEniMonitorData](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describeenimonitordata)：查询辅助网卡监控数据。

注意事项：默认查询网卡属性；指定 `--with-monitor` 时附带查询网卡监控数据。

## `ecctl ecs eni list`

调用 API：

- [DescribeNetworkInterfaces](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describenetworkinterfaces)：查看弹性网卡（ENI）列表。

分页约束：`DescribeNetworkInterfaces` 使用 `MaxResults/NextToken`，`list` 暴露 `--next-token/--limit`。`MaxResults` 最大值为 500，默认 `--limit 100`。

## `ecctl ecs eni attach`

调用 API：

- [AttachNetworkInterface](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-attachnetworkinterface)：附加弹性网卡到专有网络 VPC 类型实例上。
- [DescribeNetworkInterfaceAttribute](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describenetworkinterfaceattribute)：查询附加后的弹性网卡状态。

注意事项：该 API 的授权资源还涉及 `eni`、`instance`；这里按 `eni` 侧操作归属。命令形态对齐 `disk attach`，使用 `ecctl ecs eni attach <eni-id> --instance <instance-id>`，附加选项包括网卡索引、Trunk 实例和等待网络配置就绪。附加是异步操作，默认等待弹性网卡进入已使用状态并回读网卡视图。

## `ecctl ecs eni detach`

调用 API：

- [DetachNetworkInterface](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-detachnetworkinterface)：从实例上分离弹性网卡（ENI）。
- [DescribeNetworkInterfaceAttribute](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describenetworkinterfaceattribute)：查询分离后的弹性网卡状态。

注意事项：该 API 的授权资源还涉及 `eni`、`instance`；这里按 `eni` 侧操作归属。命令形态对齐 `disk detach`，使用 `ecctl ecs eni detach <eni-id> --instance <instance-id>`，可携带 Trunk 实例参数。分离是异步操作，默认等待弹性网卡进入可用状态并回读网卡视图。

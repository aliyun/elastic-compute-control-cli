# vpc vswitch

资源：交换机

优先级：P0

别名：`vsw`

本文件描述 `ecctl vpc vswitch` 的 interface 级命令设计：每个操作命令对应哪些 VPC API，不展开完整参数结构和输出结构；多 API 命令需写明触发 flag。

## `ecctl vpc vswitch create`

调用 API：

- [CreateVSwitch](https://help.aliyun.com/zh/vpc/developer-reference/api-vpc-2016-04-28-createvswitch)：创建交换机。
- [DescribeVSwitchAttributes](https://help.aliyun.com/zh/vpc/developer-reference/api-vpc-2016-04-28-describevswitchattributes)：回读交换机资源视图。

注意事项：创建是异步操作，默认等待交换机进入可用状态并回读。

## `ecctl vpc vswitch update`

调用 API：

- [ModifyVSwitchAttribute](https://help.aliyun.com/zh/vpc/developer-reference/api-vpc-2016-04-28-modifyvswitchattribute)：修改交换机属性。
- [DescribeVSwitchAttributes](https://help.aliyun.com/zh/vpc/developer-reference/api-vpc-2016-04-28-describevswitchattributes)：回读交换机资源视图。

注意事项：修改后默认等待交换机进入可用状态并回读。

## `ecctl vpc vswitch delete`

调用 API：

- [DeleteVSwitch](https://help.aliyun.com/zh/vpc/developer-reference/api-vpc-2016-04-28-deletevswitch)：删除交换机。
- [DescribeVSwitches](https://help.aliyun.com/zh/vpc/developer-reference/api-vpc-2016-04-28-describevswitches)：确认交换机删除完成。

注意事项：删除是异步操作，默认等待交换机不可见。

## `ecctl vpc vswitch get`

调用 API：

- [DescribeVSwitchAttributes](https://help.aliyun.com/zh/vpc/developer-reference/api-vpc-2016-04-28-describevswitchattributes)：查询指定交换机的配置信息。

## `ecctl vpc vswitch list`

调用 API：

- [DescribeVSwitches](https://help.aliyun.com/zh/vpc/developer-reference/api-vpc-2016-04-28-describevswitches)：查询交换机列表。

分页约束：`DescribeVSwitches` 的 `PageSize` 最大值为 50，`list` 默认 `--limit 50`。

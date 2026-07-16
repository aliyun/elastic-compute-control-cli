# vpc vpc

资源：专有网络 VPC

优先级：P0

本文件描述 `ecctl vpc` 的 interface 级命令设计：每个操作命令对应哪些 VPC API，不展开完整参数结构和输出结构；多 API 命令需写明触发 flag。

## `ecctl vpc create`

调用 API：

- [CreateVpc](https://help.aliyun.com/zh/vpc/developer-reference/api-vpc-2016-04-28-createvpc)：创建一个 VPC。
- [DescribeVpcAttribute](https://help.aliyun.com/zh/vpc/developer-reference/api-vpc-2016-04-28-describevpcattribute)：回读 VPC 资源视图。

注意事项：创建是异步操作，默认等待 VPC 进入可用状态并回读。

## `ecctl vpc update`

调用 API：

- [ModifyVpcAttribute](https://help.aliyun.com/zh/vpc/developer-reference/api-vpc-2016-04-28-modifyvpcattribute)：修改 VPC 属性。
- [DescribeVpcAttribute](https://help.aliyun.com/zh/vpc/developer-reference/api-vpc-2016-04-28-describevpcattribute)：回读 VPC 资源视图。

注意事项：修改后默认等待 VPC 进入可用状态并回读。

## `ecctl vpc delete`

调用 API：

- [DeleteVpc](https://help.aliyun.com/zh/vpc/developer-reference/api-vpc-2016-04-28-deletevpc)：删除一个 VPC。
- [DescribeVpcs](https://help.aliyun.com/zh/vpc/developer-reference/api-vpc-2016-04-28-describevpcs)：确认 VPC 删除完成。

注意事项：删除是异步操作，默认等待 VPC 不可见。`--force` 默认 false，指定时强制删除 VPC。

## `ecctl vpc get`

调用 API：

- [DescribeVpcAttribute](https://help.aliyun.com/zh/vpc/developer-reference/api-vpc-2016-04-28-describevpcattribute)：查询指定 VPC 的配置信息。

## `ecctl vpc list`

调用 API：

- [DescribeVpcs](https://help.aliyun.com/zh/vpc/developer-reference/api-vpc-2016-04-28-describevpcs)：查询已创建的 VPC。

分页约束：`DescribeVpcs` 的 `PageSize` 最大值为 50，`list` 默认 `--limit 50`。

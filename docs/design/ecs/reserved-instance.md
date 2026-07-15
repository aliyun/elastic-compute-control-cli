# ecs reserved-instance

资源：预留实例券

优先级：P3

别名：`ri`

本文件只描述 `ecctl ecs reserved-instance` 的 interface 级命令设计：每个操作命令对应哪些 ECS API，不展开 flag、参数结构和输出结构。

## `ecctl ecs reserved-instance create`

调用 API：

- [PurchaseReservedInstancesOffering](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-purchasereservedinstancesoffering)：购买预留实例券。

## `ecctl ecs reserved-instance update`

调用 API：

- [ModifyReservedInstanceAttribute](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-modifyreservedinstanceattribute)：修改预留实例券属性。
- [ModifyReservedInstanceAutoRenewAttribute](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-modifyreservedinstanceautorenewattribute)：修改预留实例券自动续费属性。
- [ModifyReservedInstances](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-modifyreservedinstances)：修改预留实例券配置。
- [DescribeReservedInstances](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describereservedinstances)：回读预留实例券详细信息。
- [DescribeReservedInstanceAutoRenewAttribute](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describereservedinstanceautorenewattribute)：回读预留实例券自动续费属性。

注意事项：指定 `--auto-renew` 时调用 `ModifyReservedInstanceAutoRenewAttribute`。指定 `--config` 时调用 `ModifyReservedInstances`。涉及预留实例券配置的异步变更时，默认等待目标状态并回读资源视图；自动续费变更默认回读自动续费属性。

## `ecctl ecs reserved-instance get`

调用 API：

- [DescribeReservedInstances](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describereservedinstances)：查询预留实例券详细信息列表。
- [DescribeReservedInstanceAutoRenewAttribute](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describereservedinstanceautorenewattribute)：查询预留实例券自动续费属性。

注意事项：该查询复用列表 API，通过资源标识或过滤条件收敛到单个资源。 指定 `--with-auto-renew` 时附带查询自动续费属性。

## `ecctl ecs reserved-instance list`

调用 API：

- [DescribeReservedInstances](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describereservedinstances)：查询预留实例券详细信息列表。

## `ecctl ecs reserved-instance renew`

调用 API：

- [RenewReservedInstances](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-renewreservedinstances)：续费预留实例券。

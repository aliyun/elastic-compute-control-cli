# ecs storage-capacity-unit

资源：存储容量单位包

优先级：P3

别名：`scu`

本文件只描述 `ecctl ecs storage-capacity-unit` 的 interface 级命令设计：每个操作命令对应哪些 ECS API，不展开 flag、参数结构和输出结构。

## `ecctl ecs storage-capacity-unit create`

调用 API：

- [PurchaseStorageCapacityUnit](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-purchasestoragecapacityunit)：购买存储容量单位包。

## `ecctl ecs storage-capacity-unit update`

调用 API：

- [ModifyStorageCapacityUnitAttribute](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-modifystoragecapacityunitattribute)：修改存储容量单位包属性。
- [DescribeStorageCapacityUnits](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describestoragecapacityunits)：回读存储容量单位包详细信息。

注意事项：存储容量单位包修改后默认回读资源视图，确认变更结果可见。

## `ecctl ecs storage-capacity-unit get`

调用 API：

- [DescribeStorageCapacityUnits](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describestoragecapacityunits)：查询存储容量单位包详细信息列表。

注意事项：该 API 通过容量包 ID 或过滤条件收敛到单个容量包。

## `ecctl ecs storage-capacity-unit list`

调用 API：

- [DescribeStorageCapacityUnits](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describestoragecapacityunits)：查询存储容量单位包详细信息列表。

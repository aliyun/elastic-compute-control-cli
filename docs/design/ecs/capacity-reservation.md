# ecs capacity-reservation

资源：容量预定

优先级：P3

别名：`crp`

本文件只描述 `ecctl ecs capacity-reservation` 的 interface 级命令设计：每个操作命令对应哪些 ECS API，不展开 flag、参数结构和输出结构。

## `ecctl ecs capacity-reservation create`

调用 API：

- [CreateCapacityReservation](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-createcapacityreservation)：创建容量预定服务。

## `ecctl ecs capacity-reservation update`

调用 API：

- [ModifyCapacityReservation](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-modifycapacityreservation)：修改一个容量预定服务的部分信息。
- [ModifyInstanceAttachmentAttributes](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-modifyinstanceattachmentattributes)：修改实例的私有池的属性。
- [DescribeCapacityReservations](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describecapacityreservations)：回读容量预定服务状态和资源视图。
- [DescribeInstanceAttachmentAttributes](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describeinstanceattachmentattributes)：回读实例匹配的私有池信息。
- [DescribeCapacityReservationInstances](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describecapacityreservationinstances)：回读容量预定已匹配实例列表。

注意事项：指定 `--instance-attachment` 时调用 `ModifyInstanceAttachmentAttributes`。涉及容量预定状态或实例匹配关系的异步变更时，默认等待目标状态并回读容量预定和匹配关系。

## `ecctl ecs capacity-reservation delete`

调用 API：

- [ReleaseCapacityReservation](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-releasecapacityreservation)：释放容量预定服务。

## `ecctl ecs capacity-reservation get`

调用 API：

- [DescribeCapacityReservations](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describecapacityreservations)：查询容量预定服务的信息。
- [DescribeInstanceAttachmentAttributes](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describeinstanceattachmentattributes)：查询实例匹配的私有池信息。
- [DescribeCapacityReservationInstances](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describecapacityreservationinstances)：查询容量预定服务已匹配的实例列表。

注意事项：该查询复用列表 API，通过资源标识或过滤条件收敛到单个资源。 指定 `--with-instance-attachment` 时附带查询实例匹配的私有池信息。 指定 `--with-instances` 时附带查询容量预定关联实例。

## `ecctl ecs capacity-reservation list`

调用 API：

- [DescribeCapacityReservations](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describecapacityreservations)：查询容量预定服务的信息。

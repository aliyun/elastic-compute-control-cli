# ecs managed-instance

资源：云助手托管实例

优先级：P3

别名：`mi`

本文件只描述 `ecctl ecs managed-instance` 的 interface 级命令设计：每个操作命令对应哪些 ECS API，不展开 flag、参数结构和输出结构。

## `ecctl ecs managed-instance update`

调用 API：

- [ModifyManagedInstance](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-modifymanagedinstance)：修改托管实例。
- [DescribeManagedInstances](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describemanagedinstances)：回读托管实例。

注意事项：托管实例修改后默认回读资源视图，确认变更结果可见。

## `ecctl ecs managed-instance delete`

调用 API：

- [DeregisterManagedInstance](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-deregistermanagedinstance)：注销托管实例。

## `ecctl ecs managed-instance get`

调用 API：

- [DescribeManagedInstances](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describemanagedinstances)：获取托管实例。

注意事项：该查询复用列表 API，通过资源标识或过滤条件收敛到单个资源。

## `ecctl ecs managed-instance list`

调用 API：

- [DescribeManagedInstances](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describemanagedinstances)：获取托管实例。

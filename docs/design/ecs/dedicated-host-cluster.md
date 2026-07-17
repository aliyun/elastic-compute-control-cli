# ecs dedicated-host-cluster

资源：专有宿主机组

优先级：P3

别名：`dc`

本文件只描述 `ecctl ecs dedicated-host-cluster` 的 interface 级命令设计：每个操作命令对应哪些 ECS API，不展开 flag、参数结构和输出结构。

## `ecctl ecs dedicated-host-cluster create`

调用 API：

- [CreateDedicatedHostCluster](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-creatededicatedhostcluster)：创建专有宿主机组。

## `ecctl ecs dedicated-host-cluster update`

调用 API：

- [ModifyDedicatedHostClusterAttribute](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-modifydedicatedhostclusterattribute)：修改专有宿主机组的信息。
- [DescribeDedicatedHostClusters](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describededicatedhostclusters)：回读专有宿主机组详情。

注意事项：专有宿主机组修改后默认回读资源视图，确认变更结果可见。

## `ecctl ecs dedicated-host-cluster delete`

调用 API：

- [DeleteDedicatedHostCluster](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-deletededicatedhostcluster)：删除专有宿主机组。

## `ecctl ecs dedicated-host-cluster get`

调用 API：

- [DescribeDedicatedHostClusters](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describededicatedhostclusters)：查询专有宿主机组的详情。

注意事项：该查询复用列表 API，通过资源标识或过滤条件收敛到单个资源。

## `ecctl ecs dedicated-host-cluster list`

调用 API：

- [DescribeDedicatedHostClusters](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describededicatedhostclusters)：查询专有宿主机组的详情。

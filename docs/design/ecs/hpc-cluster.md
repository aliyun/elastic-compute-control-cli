# ecs hpc-cluster

资源：HPC 集群

优先级：P3

别名：`hpc`

本文件只描述 `ecctl ecs hpc-cluster` 的 interface 级命令设计：每个操作命令对应哪些 ECS API，不展开 flag、参数结构和输出结构。

## `ecctl ecs hpc-cluster create`

调用 API：

- [CreateHpcCluster](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-createhpccluster)：创建一个 HPC 集群。

## `ecctl ecs hpc-cluster update`

调用 API：

- [ModifyHpcClusterAttribute](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-modifyhpcclusterattribute)：修改一个 HPC 集群的描述信息。
- [DescribeHpcClusters](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describehpcclusters)：回读 HPC 集群。

注意事项：HPC 集群修改后默认回读资源视图，确认变更结果可见。

## `ecctl ecs hpc-cluster delete`

调用 API：

- [DeleteHpcCluster](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-deletehpccluster)：删除一个 HPC 集群。

## `ecctl ecs hpc-cluster get`

调用 API：

- [DescribeHpcClusters](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describehpcclusters)：查询 HPC 集群。

注意事项：该查询复用列表 API，通过资源标识或过滤条件收敛到单个资源。

## `ecctl ecs hpc-cluster list`

调用 API：

- [DescribeHpcClusters](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describehpcclusters)：查询 HPC 集群。

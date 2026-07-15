# ecs task

资源：异步任务

优先级：P2

本文件只描述 `ecctl ecs task` 的 interface 级命令设计：每个操作命令对应哪些 ECS API，不展开 flag、参数结构和输出结构。

## `ecctl ecs task get`

调用 API：

- [DescribeTaskAttribute](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describetaskattribute)：查询任务详细信息。

## `ecctl ecs task list`

调用 API：

- [DescribeTasks](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describetasks)：查询任务列表。

## `ecctl ecs task cancel`

调用 API：

- [CancelTask](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-canceltask)：取消任务。
- [DescribeTaskAttribute](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describetaskattribute)：查询任务取消后的状态。

注意事项：任务取消不是资源删除，使用 `cancel` 作为动作。取消是异步操作，默认等待任务进入取消或终止状态并回读任务视图。

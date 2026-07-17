# ecs snapshot-group

资源：快照一致性组

优先级：P1

别名：`ssg`

本文件只描述 `ecctl ecs snapshot-group` 的 interface 级命令设计：每个操作命令对应哪些 ECS API，不展开 flag、参数结构和输出结构。

## `ecctl ecs snapshot-group create`

调用 API：

- [CreateSnapshotGroup](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-createsnapshotgroup)：创建快照一致性组。
- [DescribeSnapshotGroups](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describesnapshotgroups)：回读快照一致性组状态和资源视图。

注意事项：创建快照一致性组是异步操作，默认等待快照一致性组进入完成状态并回读资源视图。

## `ecctl ecs snapshot-group update`

调用 API：

- [ModifySnapshotGroup](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-modifysnapshotgroup)：修改快照一致性组。
- [DescribeSnapshotGroups](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describesnapshotgroups)：回读快照一致性组。

注意事项：快照一致性组修改后默认回读资源视图，确认变更结果可见。

## `ecctl ecs snapshot-group delete`

调用 API：

- [DeleteSnapshotGroup](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-deletesnapshotgroup)：删除快照一致性组。
- [DescribeSnapshotGroups](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describesnapshotgroups)：确认快照一致性组删除完成。

注意事项：删除是异步操作时，默认等待快照一致性组不可见或进入删除终态。

## `ecctl ecs snapshot-group get`

调用 API：

- [DescribeSnapshotGroups](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describesnapshotgroups)：查询快照一致性组。

注意事项：该查询复用列表 API，通过资源标识或过滤条件收敛到单个资源。

## `ecctl ecs snapshot-group list`

调用 API：

- [DescribeSnapshotGroups](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describesnapshotgroups)：查询快照一致性组。

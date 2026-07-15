# ecs snapshot

资源：快照

优先级：P1

本文件只描述 `ecctl ecs snapshot` 的 interface 级命令设计：每个操作命令对应哪些 ECS API，不展开 flag、参数结构和输出结构。

## `ecctl ecs snapshot create`

调用 API：

- [CreateSnapshot](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-createsnapshot)：创建快照。
- [DescribeSnapshots](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describesnapshots)：回读快照状态和资源视图。

注意事项：创建快照是异步操作，默认等待快照进入完成状态并回读快照视图。

## `ecctl ecs snapshot update`

调用 API：

- [ModifySnapshotAttribute](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-modifysnapshotattribute)：修改快照属性。
- [ModifySnapshotCategory](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-modifysnapshotcategory)：修改快照类型。
- [LockSnapshot](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-locksnapshot)：锁定快照。
- [UnlockSnapshot](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-unlocksnapshot)：解锁快照。
- [OpenSnapshotService](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-opensnapshotservice)：开通快照服务。
- [DescribeSnapshots](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describesnapshots)：回读快照状态和资源视图。
- [DescribeLockedSnapshots](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describelockedsnapshots)：回读快照锁定信息。

注意事项：指定 `--category` 时调用 `ModifySnapshotCategory`。指定 `--lock` 时调用 `LockSnapshot`。指定 `--unlock` 时调用 `UnlockSnapshot`。指定 `--open-service` 时调用 `OpenSnapshotService`。涉及快照类型、锁定状态或服务开通的异步变更时，默认等待目标状态并回读快照视图；锁定状态变更默认回读锁定信息。

## `ecctl ecs snapshot delete`

调用 API：

- [DeleteSnapshot](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-deletesnapshot)：删除快照。
- [DescribeSnapshots](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describesnapshots)：确认快照删除完成。

注意事项：删除是异步操作时，默认等待快照不可见或进入删除终态。

## `ecctl ecs snapshot get`

调用 API：

- [DescribeSnapshots](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describesnapshots)：查询云盘快照列表。
- [DescribeLockedSnapshots](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describelockedsnapshots)：查询快照锁定信息。
- [DescribeSnapshotLinks](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describesnapshotlinks)：查询云盘快照链。
- [DescribeSnapshotMonitorData](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describesnapshotmonitordata)：查询近 30 天内快照容量变化监控数据。
- [DescribeSnapshotPackage](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describesnapshotpackage)：查询某地域下已购买对象存储 OSS 存储包。
- [DescribeSnapshotsUsage](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describesnapshotsusage)：查询快照数量和容量。

注意事项：该查询复用列表 API，通过资源标识或过滤条件收敛到单个资源。 指定 `--with-lock` 时附带查询快照锁定信息。 指定 `--with-links` 时附带查询快照链，`DescribeSnapshotLinks` probe 使用 `MaxResults/NextToken`。 指定 `--with-monitor` 时附带查询快照容量监控数据。 指定 `--with-package` 时附带查询快照存储包。 指定 `--with-usage` 时附带查询快照数量和容量。

## `ecctl ecs snapshot list`

调用 API：

- [DescribeSnapshots](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describesnapshots)：查询云盘快照列表。

分页约束：`DescribeSnapshots` 使用 `MaxResults/NextToken`，`list` 暴露 `--next-token/--limit`。`MaxResults` 最大值为 100，默认 `--limit 100`。

## `ecctl ecs snapshot copy`

调用 API：

- [CopySnapshot](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-copysnapshot)：复制快照。
- [DescribeSnapshots](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describesnapshots)：查询复制出的快照状态。

注意事项：复制快照是异步操作，默认等待目标快照进入完成状态并回读快照视图。

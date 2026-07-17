# ecs auto-snapshot-policy

资源：自动快照策略

优先级：P1

别名：`sp`

本文件只描述 `ecctl ecs auto-snapshot-policy` 的 interface 级命令设计：每个操作命令对应哪些 ECS API，不展开 flag、参数结构和输出结构。

## `ecctl ecs auto-snapshot-policy create`

调用 API：

- [CreateAutoSnapshotPolicy](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-createautosnapshotpolicy)：创建自动快照策略。

## `ecctl ecs auto-snapshot-policy update`

调用 API：

- [ModifyAutoSnapshotPolicyEx](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-modifyautosnapshotpolicyex)：修改自动快照策略。
- [ApplyAutoSnapshotPolicy](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-applyautosnapshotpolicy)：为云盘应用自动快照策略。
- [CancelAutoSnapshotPolicy](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-cancelautosnapshotpolicy)：取消云盘的自动快照策略。
- [DescribeAutoSnapshotPolicyEx](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describeautosnapshotpolicyex)：回读自动快照策略。
- [DescribeAutoSnapshotPolicyAssociations](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describeautosnapshotpolicyassociations)：回读自动快照策略关联关系。

注意事项：指定 `--attach-disk-id` 时调用 `ApplyAutoSnapshotPolicy`。指定 `--detach-disk-id` 时调用 `CancelAutoSnapshotPolicy`。策略修改后默认回读策略视图；涉及关联或取消关联的异步变更时，默认等待关联关系可见并回读关联关系。

## `ecctl ecs auto-snapshot-policy delete`

调用 API：

- [DeleteAutoSnapshotPolicy](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-deleteautosnapshotpolicy)：删除自动快照策略。

## `ecctl ecs auto-snapshot-policy get`

调用 API：

- [DescribeAutoSnapshotPolicyEx](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describeautosnapshotpolicyex)：查询已创建的自动快照策略。
- [DescribeAutoSnapshotPolicyAssociations](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describeautosnapshotpolicyassociations)：查询自动快照策略的关联关系。

注意事项：该查询复用列表 API，通过资源标识或过滤条件收敛到单个资源。 指定 `--with-associations` 时附带查询策略关联关系；关联关系 probe 使用 `MaxResults/NextToken`。

## `ecctl ecs auto-snapshot-policy list`

调用 API：

- [DescribeAutoSnapshotPolicyEx](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describeautosnapshotpolicyex)：查询已创建的自动快照策略。

分页约束：`DescribeAutoSnapshotPolicyEx` 只支持 `PageNumber/PageSize`，`list` 保持 `--page/--limit`。

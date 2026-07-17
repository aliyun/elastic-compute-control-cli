# ecs disk

资源：云盘

优先级：P0

本文件只描述 `ecctl ecs disk` 的 interface 级命令设计：每个操作命令对应哪些 ECS API，不展开 flag、参数结构和输出结构。

## `ecctl ecs disk create`

调用 API：

- [CreateDisk](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-createdisk)：创建数据盘。
- [DescribeDisks](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describedisks)：回读云盘状态和资源视图。

注意事项：该 API 的授权资源还涉及 `disk`、`snapshot`；这里按 `disk` 侧操作归属。创建是异步操作，默认等待云盘进入可用状态并回读云盘视图。

## `ecctl ecs disk update`

调用 API：

- [ModifyDiskAttribute](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-modifydiskattribute)：修改块存储属性。
- [ResizeDisk](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-resizedisk)：扩容磁盘。
- [ModifyDiskSpec](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-modifydiskspec)：变更云盘类型或性能级别。
- [ModifyDiskChargeType](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-modifydiskchargetype)：修改云盘的计费方式。
- [ModifyDiskDeployment](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-modifydiskdeployment)：迁移云盘。
- [EnableDiskEncryptionByDefault](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-enablediskencryptionbydefault)：开启块存储账号级默认加密。
- [DisableDiskEncryptionByDefault](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-disablediskencryptionbydefault)：关闭块存储账号级默认加密。
- [ModifyDiskDefaultKMSKeyId](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-modifydiskdefaultkmskeyid)：修改块存储账号级默认加密使用的密钥。
- [ResetDiskDefaultKMSKeyId](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-resetdiskdefaultkmskeyid)：重置块存储账号级默认加密使用的密钥。
- [DescribeDisks](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describedisks)：回读单块云盘变更后的资源视图。

注意事项：更新单块云盘时需要云盘 ID。更新账号级默认加密配置使用 `--encryption-default enable|disable`，不需要云盘 ID，且不能和单块云盘更新参数混用；账号级默认加密相关 API 授权范围是全部资源，命令归属按 API 主对象语义确定。涉及扩容、规格、计费或部署迁移的异步变更时，默认等待目标状态并回读云盘视图。

## `ecctl ecs disk delete`

调用 API：

- [DeleteDisk](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-deletedisk)：释放按量付费数据盘。
- [DescribeDisks](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describedisks)：确认云盘删除完成。

注意事项：删除是异步操作，默认等待云盘不可见或进入删除终态。

## `ecctl ecs disk get`

调用 API：

- 默认调用 [DescribeDisks](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describedisks)：查询块存储。
- 指定 `--encryption-default` 时调用 [DescribeDiskEncryptionByDefaultStatus](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describediskencryptionbydefaultstatus)：查询块存储账号级默认加密的服务状态。
- 指定 `--default-kms-key` 时调用 [DescribeDiskDefaultKMSKeyId](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describediskdefaultkmskeyid)：查询块存储账号级默认加密使用的密钥。

注意事项：查询单块云盘时复用列表 API，通过资源标识或过滤条件收敛到单个资源。账号级默认加密服务状态和默认 KMS Key 查询必须显式指定对应 flag，不需要云盘 ID，且不能和单块云盘查询参数混用。

## `ecctl ecs disk list`

调用 API：

- [DescribeDisks](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describedisks)：查询块存储。

分页约束：`DescribeDisks` 使用 `MaxResults/NextToken`，`list` 暴露 `--next-token/--limit`。`MaxResults` 最大值为 500，默认 `--limit 100`。

## `ecctl ecs disk attach`

调用 API：

- [AttachDisk](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-attachdisk)：为实例挂载磁盘。
- [DescribeDisks](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describedisks)：查询挂载后的云盘状态。

注意事项：该 API 的授权资源还涉及 `disk`、`instance`；这里按 `disk` 侧操作归属。挂载是异步操作，默认等待云盘进入已挂载状态并回读云盘视图。

## `ecctl ecs disk detach`

调用 API：

- [DetachDisk](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-detachdisk)：卸载按量付费数据盘或系统盘。
- [DescribeDisks](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describedisks)：查询卸载后的云盘状态。

注意事项：该 API 的授权资源还涉及 `disk`、`instance`；这里按 `disk` 侧操作归属。卸载是异步操作，默认等待云盘进入可用状态并回读云盘视图。

## `ecctl ecs disk clone`

调用 API：

- [CloneDisks](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-clonedisks)：云盘克隆。
- [DescribeTasks](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describetasks)：查询克隆任务组状态。

注意事项：`CloneDisks` 返回任务组 ID，而不是直接返回克隆出的云盘 ID。克隆是异步操作，默认等待任务组完成并输出任务组信息；如后续 API 响应能稳定提供克隆盘 ID，再补充云盘视图回读。

## `ecctl ecs disk monitor`

调用 API：

- [DescribeDiskMonitorData](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describediskmonitordata)：查询云盘监控数据。

## `ecctl ecs disk reinit`

调用 API：

- [ReInitDisk](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-reinitdisk)：初始化磁盘至创建时的初始状态。
- [DescribeDisks](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describedisks)：查询重新初始化后的云盘状态。

注意事项：重新初始化是异步操作，默认等待目标状态并回读云盘视图。

## `ecctl ecs disk reset`

调用 API：

- [ResetDisk](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-resetdisk)：使用快照回滚云盘。
- [DescribeDisks](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describedisks)：查询回滚后的云盘状态。

注意事项：该 API 的授权资源还涉及 `disk`、`snapshot`；这里按 `disk` 侧操作归属。回滚是异步操作，默认等待目标状态并回读云盘视图。

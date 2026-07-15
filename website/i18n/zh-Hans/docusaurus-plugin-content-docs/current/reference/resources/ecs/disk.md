---
title: ecs disk
sidebar_label: disk
description: "管理云盘资源"
---

# ecs disk

管理云盘资源

运行 `ecctl ecs disk <action> -h` 查看用法，或 `ecctl schema ecs.disk.<action> --full` 获取该命令完整的结构化规格——每个参数与行为，便于 Agent 读取和填参。

## create

```bash
ecctl ecs disk create [flags]
```

创建云盘

- 类型：`mutation` · 风险：medium
- 同步：等待 `Available`（waiter `available_after_create`，超时 `300s`）；用 `--no-wait` 跳过等待。
- 通过 `ClientToken` 幂等。

| API | 调用时机 | 用途 |
|---|---|---|
| `CreateDisk` | 每次执行命令时 | 执行资源操作。 |
| `DescribeDisks` | 未指定 `--no-wait` 时 | 轮询等待资源达到目标状态。（重复调用） |
| `DescribeDisks` | 未指定 `--no-wait` 时 | 返回最终资源视图。（复用等待结果，不额外请求） |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--bursting-enabled` | boolean |  | 启用性能突发 |
| `--category` | string |  | 云盘类型 |
| `--description` | string |  | 云盘描述 |
| `--encrypted` | boolean |  | 加密云盘 |
| `--kms-key-id` | string |  | 云盘加密使用的 KMS 密钥 ID |
| `--name` | string |  | 云盘名称 |
| `--performance-level` | string |  | ESSD 性能等级 |
| `--provisioned-iops` | integer |  | 预配置 IOPS |
| `--resource-group` | string |  | 资源组 ID |
| `--size` | integer |  | 云盘容量，单位 GiB |
| `--snapshot` | string |  | 快照 ID |
| `--tag` | key_value |  | 标签赋值 key=value |
| `--zone` | string |  | 可用区 ID |

## update

```bash
ecctl ecs disk update <id> [flags]
```

更新云盘

- 类型：`mutation` · 风险：medium
- 通过 `ClientToken` 幂等。

| API | 调用时机 | 用途 |
|---|---|---|
| `ModifyDiskAttribute` | 指定 `--name` 或指定 `--description` 或指定 `--delete-with-instance` 或指定 `--delete-auto-snapshot` 时 | 执行资源操作。 |
| `ResizeDisk` | 指定 `--size` 时 | 执行资源操作。 |
| `ModifyDiskSpec` | 指定 `--category` 或指定 `--performance-level` 或指定 `--provisioned-iops` 或指定 `--bursting-enabled` 时 | 执行资源操作。 |
| `ModifyDiskChargeType` | 指定 `--charge-type` 时 | 执行资源操作。 |
| `ModifyDiskDeployment` | 指定 `--storage-cluster` 时 | 执行资源操作。 |
| `EnableDiskEncryptionByDefault` | `--encryption-default` 等于 `enable` 时 | 执行资源操作。 |
| `DisableDiskEncryptionByDefault` | `--encryption-default` 等于 `disable` 时 | 执行资源操作。 |
| `ModifyDiskDefaultKMSKeyId` | 指定 `--default-kms-key-id` 时 | 执行资源操作。 |
| `ResetDiskDefaultKMSKeyId` | 指定 `--reset-default-kms-key` 时 | 执行资源操作。 |
| `DescribeDisks` | 指定 `&lt;id>` 且未指定 `--no-wait` 时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--auto-pay` | boolean |  | 自动支付预付费云盘 |
| `--bursting-enabled` | boolean |  | 启用性能突发 |
| `--category` | string |  | 云盘类型 |
| `--charge-type` | string |  | 云盘计费方式 |
| `--default-kms-key-id` | string |  | 账号级云盘默认加密 KMS 密钥 ID |
| `--delete-auto-snapshot` | boolean |  | 随云盘删除自动快照 |
| `--delete-with-instance` | boolean |  | 随实例释放云盘 |
| `--description` | string |  | 云盘描述 |
| `--encryption-default` | string |  | 开启或关闭账号级云盘默认加密 |
| `--instance` | string |  | 实例 ID |
| `--name` | string |  | 云盘名称 |
| `--performance-level` | string |  | ESSD 性能等级 |
| `--provisioned-iops` | integer |  | 预配置 IOPS |
| `--reset-default-kms-key` | boolean |  | 重置账号级云盘默认加密 KMS 密钥 |
| `--resize-type` | string |  | 扩容模式 |
| `--size` | integer |  | 云盘容量，单位 GiB |
| `--storage-cluster` | string |  | 云盘迁移目标存储集群 ID |

## delete

```bash
ecctl ecs disk delete <id> [flags]
```

删除云盘

- 类型：`mutation` · 风险：high
- 同步：等待 `absent`（waiter `deleted_after_delete`，超时 `300s`）；用 `--no-wait` 跳过等待。

| API | 调用时机 | 用途 |
|---|---|---|
| `DeleteDisk` | 每次执行命令时 | 执行资源操作。 |
| `DescribeDisks` | 未指定 `--no-wait` 时 | 轮询等待资源达到目标状态。（重复调用） |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--force` | boolean |  | 在 API 支持时强制执行云盘操作（默认：`false`） |

## get

```bash
ecctl ecs disk get <id> [flags]
```

获取云盘

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `DescribeDiskEncryptionByDefaultStatus` | 指定 `--encryption-default` 时 | 读取资源视图。 |
| `DescribeDiskDefaultKMSKeyId` | 指定 `--default-kms-key` 时 | 读取资源视图。 |
| `DescribeDisks` | 指定 `&lt;id>` 时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--default-kms-key` | boolean |  | 查询账号级云盘默认加密 KMS 密钥 |
| `--encryption-default` | boolean |  | 查询账号级云盘默认加密状态 |
| `--fields` | string |  | 要包含的资源字段，使用逗号分隔 |

## list

```bash
ecctl ecs disk list [<ids>...] [flags]
```

列出云盘

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `DescribeDisks` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | 要包含的资源字段，使用逗号分隔 |
| `--filter` | key_value |  | 过滤表达式 key=value |
| `--limit` | integer |  | 最多返回资源数量（默认：`100`） |
| `--next-token` | string |  | 下一页查询 token |

## attach

```bash
ecctl ecs disk attach <id> [flags]
```

挂载云盘

- 类型：`mutation` · 风险：medium
- 同步：等待 `In_use`（waiter `in_use_after_attach`，超时 `300s`）；用 `--no-wait` 跳过等待。

| API | 调用时机 | 用途 |
|---|---|---|
| `AttachDisk` | 每次执行命令时 | 执行资源操作。 |
| `DescribeDisks` | 未指定 `--no-wait` 时 | 轮询等待资源达到目标状态。（重复调用） |
| `DescribeDisks` | 未指定 `--no-wait` 时 | 返回最终资源视图。（复用等待结果，不额外请求） |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--instance` | string | ✓ | 实例 ID |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--delete-with-instance` | boolean |  | 随实例释放云盘 |
| `--device` | string |  | 实例中的设备名 |
| `--force` | boolean |  | 在 API 支持时强制执行云盘操作（默认：`false`） |

## clone

```bash
ecctl ecs disk clone <id> [flags]
```

克隆云盘

- 类型：`mutation` · 风险：medium
- 同步：等待 `Finished`（waiter `clone_finished`，超时 `3600s`）；用 `--no-wait` 跳过等待。
- 通过 `ClientToken` 幂等。

| API | 调用时机 | 用途 |
|---|---|---|
| `CloneDisks` | 每次执行命令时 | 执行资源操作。 |
| `DescribeTasks` | 未指定 `--no-wait` 时 | 轮询等待资源达到目标状态。（重复调用） |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--category` | string | ✓ | 云盘类型 |
| `--multi-attach` | string | ✓ | 多重挂载模式 |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--size` | integer | ✓ | 云盘容量，单位 GiB |
| `--bursting-enabled` | boolean |  | 启用性能突发 |
| `--encrypted` | boolean |  | 加密云盘 |
| `--kms-key-id` | string |  | 云盘加密使用的 KMS 密钥 ID |
| `--name` | string |  | 云盘名称 |
| `--performance-level` | string |  | ESSD 性能等级 |
| `--provisioned-iops` | integer |  | 预配置 IOPS |
| `--resource-group` | string |  | 资源组 ID |
| `--tag` | key_value |  | 标签赋值 key=value |

## detach

```bash
ecctl ecs disk detach <id> [flags]
```

卸载云盘

- 类型：`mutation` · 风险：medium
- 同步：等待 `Available`（waiter `available_after_detach`，超时 `300s`）；用 `--no-wait` 跳过等待。

| API | 调用时机 | 用途 |
|---|---|---|
| `DetachDisk` | 每次执行命令时 | 执行资源操作。 |
| `DescribeDisks` | 未指定 `--no-wait` 时 | 轮询等待资源达到目标状态。（重复调用） |
| `DescribeDisks` | 未指定 `--no-wait` 时 | 返回最终资源视图。（复用等待结果，不额外请求） |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--instance` | string | ✓ | 实例 ID |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--delete-with-instance` | boolean |  | 随实例释放云盘 |

## monitor

```bash
ecctl ecs disk monitor <id> [flags]
```

查询云盘监控数据

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `DescribeDiskMonitorData` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--end-time` | string | ✓ | 监控结束时间 |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--start-time` | string | ✓ | 监控开始时间 |
| `--period-seconds` | integer |  | 监控周期，单位秒 |

## reinit

```bash
ecctl ecs disk reinit <id> [flags]
```

重新初始化云盘

- 类型：`mutation` · 风险：medium

| API | 调用时机 | 用途 |
|---|---|---|
| `ReInitDisk` | 每次执行命令时 | 执行资源操作。 |
| `DescribeDisks` | 未指定 `--no-wait` 时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |

## reset

```bash
ecctl ecs disk reset <id> [flags]
```

使用快照回滚云盘

- 类型：`mutation` · 风险：medium

| API | 调用时机 | 用途 |
|---|---|---|
| `ResetDisk` | 每次执行命令时 | 执行资源操作。 |
| `DescribeDisks` | 未指定 `--no-wait` 时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--snapshot` | string | ✓ | 快照 ID |

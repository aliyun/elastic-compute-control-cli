---
title: ecs snapshot-group
sidebar_label: snapshot-group
description: "管理快照一致性组"
---

# ecs snapshot-group

管理快照一致性组

运行 `ecctl ecs snapshot-group <action> -h` 查看用法，或 `ecctl schema ecs.snapshot-group.<action> --full` 获取该命令完整的结构化规格——每个参数与行为，便于 Agent 读取和填参。

## create

```bash
ecctl ecs snapshot-group create [flags]
```

创建快照一致性组

- 类型：`mutation` · 风险：medium
- 同步：等待 `accomplished`（waiter `accomplished_after_create`，超时 `600s`）；用 `--no-wait` 跳过等待。
- 通过 `ClientToken` 幂等。

| API | 调用时机 | 用途 |
|---|---|---|
| `CreateSnapshotGroup` | 每次执行命令时 | 执行资源操作。 |
| `DescribeSnapshotGroups` | 未指定 `--no-wait` 时 | 轮询等待资源达到目标状态。（重复调用） |
| `DescribeSnapshotGroups` | 未指定 `--no-wait` 时 | 返回最终资源视图。（复用等待结果，不额外请求） |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--instance` | string | ✓ | 源实例 ID |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--description` | string |  | 快照一致性组描述 |
| `--disk-ids` | array |  | 纳入一致性组的云盘 ID 列表 |
| `--exclude-disk-ids` | array |  | 排除在一致性组之外的云盘 ID 列表 |
| `--instant-access` | boolean |  | 为组内快照启用快速可用 |
| `--instant-access-retention-days` | integer |  | 快速可用保留天数 |
| `--name` | string |  | 快照一致性组名称 |
| `--resource-group` | string |  | 资源组 ID |
| `--tag` | key_value |  | 标签赋值 key=value |

## update

```bash
ecctl ecs snapshot-group update <id> [flags]
```

更新快照一致性组

- 类型：`mutation` · 风险：medium

| API | 调用时机 | 用途 |
|---|---|---|
| `ModifySnapshotGroup` | 每次执行命令时 | 执行资源操作。 |
| `DescribeSnapshotGroups` | 未指定 `--no-wait` 时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--description` | string |  | 快照一致性组描述 |
| `--name` | string |  | 快照一致性组名称 |

## delete

```bash
ecctl ecs snapshot-group delete <id> [flags]
```

删除快照一致性组

- 类型：`mutation` · 风险：high
- 同步：等待 `absent`（waiter `deleted_after_delete`，超时 `300s`）；用 `--no-wait` 跳过等待。

| API | 调用时机 | 用途 |
|---|---|---|
| `DeleteSnapshotGroup` | 每次执行命令时 | 执行资源操作。 |
| `DescribeSnapshotGroups` | 未指定 `--no-wait` 时 | 轮询等待资源达到目标状态。（重复调用） |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |

## get

```bash
ecctl ecs snapshot-group get <id> [flags]
```

获取快照一致性组

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `DescribeSnapshotGroups` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | 要包含的资源字段，使用逗号分隔 |

## list

```bash
ecctl ecs snapshot-group list [<ids>...] [flags]
```

列出快照一致性组

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `DescribeSnapshotGroups` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | 要包含的资源字段，使用逗号分隔 |
| `--filter` | key_value |  | 过滤表达式 key=value |
| `--limit` | integer |  | 最多返回资源数量（默认：`100`） |
| `--next-token` | string |  | 下一页查询 token |

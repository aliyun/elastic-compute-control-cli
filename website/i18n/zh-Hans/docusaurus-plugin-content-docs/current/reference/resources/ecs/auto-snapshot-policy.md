---
generated: true
generated_by: "website/scripts/gen-reference.mjs"
generated_command: "make build && npm --prefix website run gen:reference"
title: ecs auto-snapshot-policy
sidebar_label: auto-snapshot-policy
description: "管理自动快照策略"
---

# ecs auto-snapshot-policy

管理自动快照策略

运行 `ecctl ecs auto-snapshot-policy <action> -h` 查看用法，或 `ecctl schema ecs.auto-snapshot-policy.<action> --full` 获取该命令完整的结构化规格——每个参数与行为，便于 Agent 读取和填参。

## create

```bash
ecctl ecs auto-snapshot-policy create [flags]
```

创建自动快照策略

- 类型：`mutation` · 风险：medium

| API | 调用时机 | 用途 |
|---|---|---|
| `CreateAutoSnapshotPolicy` | 每次执行命令时 | 执行资源操作。 |
| `DescribeAutoSnapshotPolicyEx` | 未指定 `--no-wait` 时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--repeat-weekdays` | string | ✓ | 重复执行的星期 JSON 数组，例如 '["1","7"]' |
| `--retention-days` | integer | ✓ | 快照保留天数（-1 表示永久保留） |
| `--time-points` | string | ✓ | 创建快照的时间点 JSON 数组，例如 '["0","12"]' |
| `--copied-snapshots-retention-days` | integer |  | 跨地域复制快照的保留天数 |
| `--enable-cross-region-copy` | boolean |  | 启用跨地域快照复制 |
| `--name` | string |  | 自动快照策略名称 |
| `--resource-group` | string |  | 资源组 ID |
| `--tag` | key_value |  | 标签赋值 key=value |
| `--target-copy-regions` | string |  | 跨地域复制的目标地域 JSON 数组 |

## update

```bash
ecctl ecs auto-snapshot-policy update <id> [flags]
```

更新自动快照策略或其云盘关联

- 类型：`mutation` · 风险：medium

| API | 调用时机 | 用途 |
|---|---|---|
| `ModifyAutoSnapshotPolicyEx` | 指定 `--name` 或指定 `--time-points` 或指定 `--repeat-weekdays` 或指定 `--retention-days` 或指定 `--copied-snapshots-retention-days` 或指定 `--enable-cross-region-copy` 或指定 `--target-copy-regions` 时 | 执行资源操作。 |
| `ApplyAutoSnapshotPolicy` | 指定 `--attach-disk-id` 时 | 执行资源操作。 |
| `CancelAutoSnapshotPolicy` | 指定 `--detach-disk-id` 时 | 执行资源操作。 |
| `DescribeAutoSnapshotPolicyEx` | 未指定 `--no-wait` 时 | 读取资源视图。 |
| `DescribeAutoSnapshotPolicyAssociations` | 指定 `--with-associations` 且未指定 `--no-wait` 时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--attach-disk-id` | array |  | 应用该策略的云盘 ID 列表 |
| `--copied-snapshots-retention-days` | integer |  | 跨地域复制快照的保留天数 |
| `--detach-disk-id` | array |  | 取消该策略的云盘 ID 列表 |
| `--enable-cross-region-copy` | boolean |  | 启用跨地域快照复制 |
| `--name` | string |  | 自动快照策略名称 |
| `--repeat-weekdays` | string |  | 重复执行的星期 JSON 数组，例如 '["1","7"]' |
| `--retention-days` | integer |  | 快照保留天数（-1 表示永久保留） |
| `--target-copy-regions` | string |  | 跨地域复制的目标地域 JSON 数组 |
| `--time-points` | string |  | 创建快照的时间点 JSON 数组，例如 '["0","12"]' |
| `--with-associations` | boolean |  | 附带云盘关联关系 |

## delete

```bash
ecctl ecs auto-snapshot-policy delete <id> [flags]
```

删除自动快照策略

- 类型：`mutation` · 风险：high

| API | 调用时机 | 用途 |
|---|---|---|
| `DeleteAutoSnapshotPolicy` | 每次执行命令时 | 执行资源操作。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |

## get

```bash
ecctl ecs auto-snapshot-policy get <id> [flags]
```

获取自动快照策略

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `DescribeAutoSnapshotPolicyEx` | 每次执行命令时 | 读取资源视图。 |
| `DescribeAutoSnapshotPolicyAssociations` | 指定 `--with-associations` 时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | 要包含的资源字段，使用逗号分隔 |
| `--with-associations` | boolean |  | 附带云盘关联关系 |

## list

```bash
ecctl ecs auto-snapshot-policy list <id> [flags]
```

列出自动快照策略

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `DescribeAutoSnapshotPolicyEx` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | 要包含的资源字段，使用逗号分隔 |
| `--filter` | key_value |  | 过滤表达式 key=value |
| `--limit` | integer |  | 最多返回资源数量（默认：`100`） |
| `--page` | integer |  | 返回结果页码（默认：`1`） |

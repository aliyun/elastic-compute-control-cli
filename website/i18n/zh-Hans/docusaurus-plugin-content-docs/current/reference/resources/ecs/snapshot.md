---
generated: true
generated_by: "website/scripts/gen-reference.mjs"
generated_command: "make build && npm --prefix website run gen:reference"
title: ecs snapshot
sidebar_label: snapshot
description: "管理云盘快照"
---

# ecs snapshot

管理云盘快照

运行 `ecctl ecs snapshot <action> -h` 查看用法，或 `ecctl schema ecs.snapshot.<action> --full` 获取该命令完整的结构化规格——每个参数与行为，便于 Agent 读取和填参。

## create

```bash
ecctl ecs snapshot create [flags]
```

创建快照

- 类型：`mutation` · 风险：medium
- 同步：等待 `accomplished`（waiter `accomplished_after_create`，超时 `600s`）；用 `--no-wait` 跳过等待。
- 通过 `ClientToken` 幂等。

| API | 调用时机 | 用途 |
|---|---|---|
| `CreateSnapshot` | 每次执行命令时 | 执行资源操作。 |
| `DescribeSnapshots` | 未指定 `--no-wait` 时 | 轮询等待资源达到目标状态。（重复调用） |
| `DescribeSnapshots` | 未指定 `--no-wait` 时 | 返回最终资源视图。（复用等待结果，不额外请求） |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--disk` | string | ✓ | 源云盘 ID |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--category` | string |  | 快照类型（普通/极速可用） |
| `--description` | string |  | 快照描述 |
| `--instant-access` | boolean |  | 启用极速可用 |
| `--instant-access-retention-days` | integer |  | 极速可用保留天数 |
| `--name` | string |  | 快照名称 |
| `--resource-group` | string |  | 资源组 ID |
| `--retention-days` | integer |  | 快照保留天数 |
| `--tag` | key_value |  | 标签赋值 key=value |

## update

```bash
ecctl ecs snapshot update <id> [flags]
```

更新快照属性、类型或锁定状态

- 类型：`mutation` · 风险：medium

| API | 调用时机 | 用途 |
|---|---|---|
| `ModifySnapshotAttribute` | 指定 `--name` 或指定 `--description` 或指定 `--disable-instant-access` 时 | 执行资源操作。 |
| `ModifySnapshotCategory` | 指定 `--category` 时 | 执行资源操作。 |
| `LockSnapshot` | 指定 `--lock` 时 | 执行资源操作。 |
| `UnlockSnapshot` | 指定 `--unlock` 时 | 执行资源操作。 |
| `OpenSnapshotService` | 指定 `--open-service` 时 | 执行资源操作。 |
| `DescribeSnapshots` | 未指定 `--no-wait` 时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--category` | string |  | 快照类型（普通/极速可用） |
| `--description` | string |  | 快照描述 |
| `--disable-instant-access` | boolean |  | 更新时关闭极速可用 |
| `--lock` | boolean |  | 锁定快照以防删除 |
| `--name` | string |  | 快照名称 |
| `--open-service` | boolean |  | 开通快照服务 |
| `--unlock` | boolean |  | 解锁快照 |

## delete

```bash
ecctl ecs snapshot delete <id> [flags]
```

删除快照

- 类型：`mutation` · 风险：high
- 同步：等待 `absent`（waiter `deleted_after_delete`，超时 `300s`）；用 `--no-wait` 跳过等待。

| API | 调用时机 | 用途 |
|---|---|---|
| `DeleteSnapshot` | 每次执行命令时 | 执行资源操作。 |
| `DescribeSnapshots` | 未指定 `--no-wait` 时 | 轮询等待资源达到目标状态。（重复调用） |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--force` | boolean |  | 即使快照被云盘使用也强制删除（必须显式指定）（默认：`false`） |

## get

```bash
ecctl ecs snapshot get <id> [flags]
```

获取快照

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `DescribeSnapshots` | 每次执行命令时 | 读取资源视图。 |
| `DescribeLockedSnapshots` | 指定 `--with-lock` 时 | 读取资源视图。 |
| `DescribeSnapshotLinks` | 指定 `--with-links` 时 | 读取资源视图。 |
| `DescribeSnapshotMonitorData` | 指定 `--with-monitor` 时 | 读取资源视图。 |
| `DescribeSnapshotPackage` | 指定 `--with-package` 时 | 读取资源视图。 |
| `DescribeSnapshotsUsage` | 指定 `--with-usage` 时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--disk` | string |  | 源云盘 ID |
| `--end-time` | string |  | 监控查询结束时间 |
| `--fields` | string |  | 要包含的资源字段，使用逗号分隔 |
| `--instance` | string |  | 源实例 ID |
| `--period` | integer |  | 监控数据周期，单位秒 |
| `--start-time` | string |  | 监控查询开始时间 |
| `--with-links` | boolean |  | 附带快照链信息 |
| `--with-lock` | boolean |  | 附带快照锁定信息 |
| `--with-monitor` | boolean |  | 附带快照容量监控数据 |
| `--with-package` | boolean |  | 附带 OSS 快照存储包信息 |
| `--with-usage` | boolean |  | 附带快照数量和容量 |

## list

```bash
ecctl ecs snapshot list [<ids>...] [flags]
```

列出快照

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `DescribeSnapshots` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | 要包含的资源字段，使用逗号分隔 |
| `--filter` | key_value |  | 过滤表达式 key=value |
| `--limit` | integer |  | 最多返回资源数量（默认：`100`） |
| `--next-token` | string |  | 下一页查询 token |

## copy

```bash
ecctl ecs snapshot copy <id> [flags]
```

跨地域复制快照

- 类型：`mutation` · 风险：medium
- 同步：等待 `accomplished`（waiter `accomplished_after_copy`，超时 `600s`）；用 `--no-wait` 跳过等待。
- 通过 `ClientToken` 幂等。

| API | 调用时机 | 用途 |
|---|---|---|
| `CopySnapshot` | 每次执行命令时 | 执行资源操作。 |
| `DescribeSnapshots` | 未指定 `--no-wait` 时 | 轮询等待资源达到目标状态。（重复调用） |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--destination-region` | string | ✓ | 复制快照的目标地域 |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--destination-description` | string |  | 复制快照的目标描述 |
| `--destination-name` | string |  | 复制快照的目标名称 |
| `--encrypted` | boolean |  | 复制快照时加密目标快照 |
| `--kms-key-id` | string |  | 加密目标快照使用的 KMS 密钥 ID |
| `--resource-group` | string |  | 资源组 ID |
| `--retention-days` | integer |  | 快照保留天数 |
| `--tag` | key_value |  | 标签赋值 key=value |

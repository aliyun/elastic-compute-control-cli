---
generated: true
generated_by: "website/scripts/gen-reference.mjs"
generated_command: "make build && npm --prefix website run gen:reference"
title: ack inspect config
sidebar_label: inspect config
description: "管理集群巡检配置"
---

# ack inspect config

管理集群巡检配置

运行 `ecctl ack inspect config <action> -h` 查看用法，或 `ecctl schema ack.inspect.config.<action> --full` 获取该命令完整的结构化规格——每个参数与行为，便于 Agent 读取和填参。

## update

```bash
ecctl ack inspect config update [flags]
```

更新巡检配置

- 类型：`mutation` · 风险：medium
- 同步：等待 `present`（waiter `config_present`，超时 `60s`）；用 `--no-wait` 跳过等待。

| API | 调用时机 | 用途 |
|---|---|---|
| `GetClusterInspectConfig` | 每次执行命令时 | 读取资源视图。 |
| `CreateClusterInspectConfig` | 前序步骤未生成 `existing` 时 | 执行资源操作。 |
| `GetClusterInspectConfig` | 前序步骤未生成 `existing` 时 | 轮询等待资源达到目标状态。（重复调用） |
| `UpdateClusterInspectConfig` | 前序步骤已生成 `existing` 时 | 执行资源操作。 |
| `GetClusterInspectConfig` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--cluster` | string | ✓ | ACK 集群 ID |
| `--recurrence` | string | ✓ | 包含 BYHOUR 和 BYMINUTE 的 RFC5545 每日巡检规则 |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--disabled-check-items` | string_array |  | 要禁用的巡检项名称 |
| `--enabled` | boolean |  | 启用周期性巡检 |

## delete

```bash
ecctl ack inspect config delete [flags]
```

删除巡检配置

- 类型：`mutation` · 风险：high

| API | 调用时机 | 用途 |
|---|---|---|
| `DeleteClusterInspectConfig` | 每次执行命令时 | 执行资源操作。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--cluster` | string | ✓ | ACK 集群 ID |
| `--region` | string | ✓ | Alibaba Cloud region |

## get

```bash
ecctl ack inspect config get [flags]
```

查询巡检配置

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `GetClusterInspectConfig` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--cluster` | string | ✓ | ACK 集群 ID |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | 要包含的资源字段，使用逗号分隔 |

---
generated: true
generated_by: "website/scripts/gen-reference.mjs"
generated_command: "make build && npm --prefix website run gen:reference"
title: ack check
sidebar_label: check
description: "管理 ACK 集群检查报告"
---

# ack check

管理 ACK 集群检查报告

运行 `ecctl ack check <action> -h` 查看用法，或 `ecctl schema ack.check.<action> --full` 获取该命令完整的结构化规格——每个参数与行为，便于 Agent 读取和填参。

## create

```bash
ecctl ack check create [flags]
```

创建 ACK 集群检查

- 类型：`mutation` · 风险：medium
- 同步：等待 `Succeeded`（waiter `succeeded_after_create`，超时 `600s`）；用 `--no-wait` 跳过等待。

| API | 调用时机 | 用途 |
|---|---|---|
| `RunClusterCheck` | 每次执行命令时 | 执行资源操作。 |
| `GetClusterCheck` | 未指定 `--no-wait` 时 | 轮询等待资源达到目标状态。（重复调用） |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--cluster` | string | ✓ | ACK 集群 ID |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--type` | string | ✓ | 检查类型，例如 ClusterUpgrade、MasterUpgrade、NodePoolUpgrade 或 ClusterMigrate |
| `--target` | string |  | 检查目标，例如 NodePoolUpgrade 检查的节点池 ID |

## get

```bash
ecctl ack check get <id> [flags]
```

查询 ACK 集群检查

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `GetClusterCheck` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--cluster` | string | ✓ | ACK 集群 ID |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | 要包含的资源字段，使用逗号分隔 |

## list

```bash
ecctl ack check list [flags]
```

列出 ACK 集群检查

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `ListClusterChecks` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--cluster` | string | ✓ | ACK 集群 ID |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | 要包含的资源字段，使用逗号分隔 |
| `--filter` | key_value |  | 过滤表达式 key=value |
| `--limit` | integer |  | 最多返回检查数量（默认：`100`） |
| `--page` | integer |  | 返回结果页码（默认：`1`） |
| `--target` | string |  | 检查目标，例如 NodePoolUpgrade 检查的节点池 ID |
| `--type` | string |  | 检查类型，例如 ClusterUpgrade、MasterUpgrade、NodePoolUpgrade 或 ClusterMigrate |

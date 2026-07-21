---
generated: true
generated_by: "website/scripts/gen-reference.mjs"
generated_command: "make build && npm --prefix website run gen:reference"
title: ack node
sidebar_label: node
description: "管理 ACK 集群节点"
---

# ack node

管理 ACK 集群节点

运行 `ecctl ack node <action> -h` 查看用法，或 `ecctl schema ack.node.<action> --full` 获取该命令完整的结构化规格——每个参数与行为，便于 Agent 读取和填参。

## delete

```bash
ecctl ack node delete [<ids>...] [flags]
```

从 ACK 集群移除节点

- 类型：`mutation` · 风险：high
- 同步：等待 `absent`（waiter `absent_after_delete`，超时 `300s`）；用 `--no-wait` 跳过等待。

| API | 调用时机 | 用途 |
|---|---|---|
| `DeleteClusterNodes` | 每次执行命令时 | 执行资源操作。 |
| `DescribeClusterNodes` | 未指定 `--no-wait` 时 | 轮询等待资源达到目标状态。（重复调用） |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--cluster` | string | ✓ | ACK 集群 ID |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--force` | boolean |  | 调用方确认后强制执行删除命令（默认：`false`） |
| `--release` | boolean |  | 从集群移除节点后释放 ECS 实例（默认：`false`） |

## get

```bash
ecctl ack node get <node-id> [flags]
```

获取 ACK 节点

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `DescribeClusterNodes` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--cluster` | string | ✓ | ACK 集群 ID |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | 要包含的资源字段，使用逗号分隔 |

## list

```bash
ecctl ack node list [flags]
```

列出 ACK 节点

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `DescribeClusterNodes` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--cluster` | string | ✓ | ACK 集群 ID |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | 要包含的资源字段，使用逗号分隔 |
| `--limit` | integer |  | 最多返回节点数量（默认：`100`） |
| `--nodepool` | string |  | ACK 节点池 ID |
| `--page` | integer |  | 返回结果页码（默认：`1`） |

## attach

```bash
ecctl ack node attach [flags]
```

将 ECS 实例直接加入 ACK 集群

- 类型：`mutation` · 风险：medium
- 同步：等待 `Ready`（waiter `ready_after_attach`，超时 `300s`）；用 `--no-wait` 跳过等待。

| API | 调用时机 | 用途 |
|---|---|---|
| `AttachInstances` | 每次执行命令时 | 执行资源操作。 |
| `DescribeClusterNodes` | 未指定 `--no-wait` 时 | 轮询等待资源达到目标状态。（重复调用） |
| `DescribeClusterNodes` | 未指定 `--no-wait` 时 | 返回最终资源视图。（复用等待结果，不额外请求） |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--cluster` | string | ✓ | ACK 集群 ID |
| `--instance` | string | ✓ | ACK 节点 ECS 实例 ID |
| `--region` | string | ✓ | Alibaba Cloud region |

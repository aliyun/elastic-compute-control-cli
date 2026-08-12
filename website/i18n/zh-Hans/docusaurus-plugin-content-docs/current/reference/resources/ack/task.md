---
generated: true
generated_by: "website/scripts/gen-reference.mjs"
generated_command: "make build && npm --prefix website run gen:reference"
title: ack task
sidebar_label: task
description: "查询和控制 ACK 异步任务"
---

# ack task

查询和控制 ACK 异步任务

运行 `ecctl ack task <action> -h` 查看用法，或 `ecctl schema ack.task.<action> --full` 获取该命令完整的结构化规格——每个参数与行为，便于 Agent 读取和填参。

## get

```bash
ecctl ack task get <task-id> [flags]
```

查询任务详情

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `DescribeTaskInfo` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--cluster` | string |  | ACK 集群 ID |
| `--fields` | string |  | 要包含的资源字段，使用逗号分隔 |

## list

```bash
ecctl ack task list [flags]
```

列出集群任务

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `DescribeClusterTasks` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--cluster` | string | ✓ | ACK 集群 ID |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | 要包含的资源字段，使用逗号分隔 |
| `--filter` | key_value |  | 过滤表达式 key=value |
| `--limit` | integer |  | 最多返回任务数量（默认：`100`） |
| `--page` | integer |  | 返回结果页码（默认：`1`） |

## cancel

```bash
ecctl ack task cancel <task-id> [flags]
```

取消任务

- 类型：`mutation` · 风险：medium
- 同步：等待 `canceled`（waiter `canceled_after_cancel`，超时 `10m`）；用 `--no-wait` 跳过等待。

| API | 调用时机 | 用途 |
|---|---|---|
| `CancelTask` | 每次执行命令时 | 执行资源操作。 |
| `DescribeTaskInfo` | 每次执行命令时 | 轮询等待资源达到目标状态。（重复调用） |
| `DescribeTaskInfo` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |

## pause

```bash
ecctl ack task pause <task-id> [flags]
```

暂停任务

- 类型：`mutation` · 风险：medium
- 同步：等待 `paused`（waiter `paused_after_pause`，超时 `10m`）；用 `--no-wait` 跳过等待。

| API | 调用时机 | 用途 |
|---|---|---|
| `PauseTask` | 每次执行命令时 | 执行资源操作。 |
| `DescribeTaskInfo` | 每次执行命令时 | 轮询等待资源达到目标状态。（重复调用） |
| `DescribeTaskInfo` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |

## resume

```bash
ecctl ack task resume <task-id> [flags]
```

恢复任务

- 类型：`mutation` · 风险：medium
- 同步：等待 `running`（waiter `running_after_resume`，超时 `10m`）；用 `--no-wait` 跳过等待。

| API | 调用时机 | 用途 |
|---|---|---|
| `ResumeTask` | 每次执行命令时 | 执行资源操作。 |
| `DescribeTaskInfo` | 每次执行命令时 | 轮询等待资源达到目标状态。（重复调用） |
| `DescribeTaskInfo` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |

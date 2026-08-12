---
generated: true
generated_by: "website/scripts/gen-reference.mjs"
generated_command: "make build && npm --prefix website run gen:reference"
title: ack task
sidebar_label: task
description: "Query and control ACK asynchronous tasks"
---

# ack task

Query and control ACK asynchronous tasks

Run `ecctl ack task <action> -h` for usage, or `ecctl schema ack.task.<action> --full` for the complete, agent-readable spec of every parameter and behavior.

## get

```bash
ecctl ack task get <task-id> [flags]
```

Get task details

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `DescribeTaskInfo` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--cluster` | string |  | ACK cluster ID |
| `--fields` | string |  | comma-separated resource fields to include |

## list

```bash
ecctl ack task list [flags]
```

List cluster tasks

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `DescribeClusterTasks` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--cluster` | string | ✓ | ACK cluster ID |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | comma-separated resource fields to include |
| `--filter` | key_value |  | filter expression key=value |
| `--limit` | integer |  | maximum tasks to return (default: `100`) |
| `--page` | integer |  | results page to return (default: `1`) |

## cancel

```bash
ecctl ack task cancel <task-id> [flags]
```

Cancel a task

- Kind: `mutation` · Risk: medium
- Synchronous: waits for `canceled` (waiter `canceled_after_cancel`, timeout `10m`); use `--no-wait` to skip.

| API | When called | Purpose |
|---|---|---|
| `CancelTask` | Every time the command runs. | Perform the resource operation. |
| `DescribeTaskInfo` | Every time the command runs. | Poll until the resource reaches the target state. (repeated) |
| `DescribeTaskInfo` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |

## pause

```bash
ecctl ack task pause <task-id> [flags]
```

Pause a task

- Kind: `mutation` · Risk: medium
- Synchronous: waits for `paused` (waiter `paused_after_pause`, timeout `10m`); use `--no-wait` to skip.

| API | When called | Purpose |
|---|---|---|
| `PauseTask` | Every time the command runs. | Perform the resource operation. |
| `DescribeTaskInfo` | Every time the command runs. | Poll until the resource reaches the target state. (repeated) |
| `DescribeTaskInfo` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |

## resume

```bash
ecctl ack task resume <task-id> [flags]
```

Resume a task

- Kind: `mutation` · Risk: medium
- Synchronous: waits for `running` (waiter `running_after_resume`, timeout `10m`); use `--no-wait` to skip.

| API | When called | Purpose |
|---|---|---|
| `ResumeTask` | Every time the command runs. | Perform the resource operation. |
| `DescribeTaskInfo` | Every time the command runs. | Poll until the resource reaches the target state. (repeated) |
| `DescribeTaskInfo` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |

---
generated: true
generated_by: "website/scripts/gen-reference.mjs"
generated_command: "make build && npm --prefix website run gen:reference"
title: ack audit
sidebar_label: audit
description: "管理 ACK 集群 API Server 审计日志"
---

# ack audit

管理 ACK 集群 API Server 审计日志

运行 `ecctl ack audit <action> -h` 查看用法，或 `ecctl schema ack.audit.<action> --full` 获取该命令完整的结构化规格——每个参数与行为，便于 Agent 读取和填参。

## update

```bash
ecctl ack audit update [flags]
```

更新 ACK 集群 API Server 审计日志

- 类型：`mutation` · 风险：medium
- 同步：等待 `success`（waiter `task_succeeded`，超时 `600s`）；用 `--no-wait` 跳过等待。

| API | 调用时机 | 用途 |
|---|---|---|
| `UpdateClusterAuditLogConfig` | `--enabled` 等于 `true` 时 | 执行资源操作。 |
| `DescribeTaskInfo` | `--enabled` 等于 `true` 且未指定 `--no-wait` 时 | 轮询等待资源达到目标状态。（重复调用） |
| `UpdateClusterAuditLogConfig` | `--enabled` 等于 `false` 时 | 执行资源操作。 |
| `DescribeTaskInfo` | `--enabled` 等于 `false` 且未指定 `--no-wait` 时 | 轮询等待资源达到目标状态。（重复调用） |
| `UpdateClusterAuditLogConfig` | 指定 `--project` 且未显式指定 `--enabled` 时 | 执行资源操作。 |
| `DescribeTaskInfo` | 指定 `--project` 且未显式指定 `--enabled` 且未指定 `--no-wait` 时 | 轮询等待资源达到目标状态。（重复调用） |
| `GetClusterAuditProject` | 未指定 `--no-wait` 时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--cluster` | string | ✓ | ACK 集群 ID |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--enabled` | boolean |  | 开启或关闭 API Server 审计日志 |
| `--project` | string |  | API Server 审计日志所在的 SLS Project |

## get

```bash
ecctl ack audit get [flags]
```

获取 ACK 集群 API Server 审计日志配置

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `GetClusterAuditProject` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--cluster` | string | ✓ | ACK 集群 ID |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | 要包含的资源字段，使用逗号分隔 |

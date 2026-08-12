---
generated: true
generated_by: "website/scripts/gen-reference.mjs"
generated_command: "make build && npm --prefix website run gen:reference"
title: ack audit control-plane-log
sidebar_label: audit control-plane-log
description: "管理 ACK 托管集群控制面组件日志"
---

# ack audit control-plane-log

管理 ACK 托管集群控制面组件日志

运行 `ecctl ack audit control-plane-log <action> -h` 查看用法，或 `ecctl schema ack.audit.control-plane-log.<action> --full` 获取该命令完整的结构化规格——每个参数与行为，便于 Agent 读取和填参。

## update

```bash
ecctl ack audit control-plane-log update [flags]
```

更新 ACK 托管集群控制面组件日志

- 类型：`mutation` · 风险：medium
- 同步：等待 `success`（waiter `task_succeeded`，超时 `600s`）；用 `--no-wait` 跳过等待。

| API | 调用时机 | 用途 |
|---|---|---|
| `UpdateControlPlaneLog` | `--enabled` 等于 `false` 时 | 执行资源操作。 |
| `DescribeTaskInfo` | `--enabled` 等于 `false` 且未指定 `--no-wait` 时 | 轮询等待资源达到目标状态。（重复调用） |
| `UpdateControlPlaneLog` | `--enabled` 不等于 `false` 时 | 执行资源操作。 |
| `DescribeTaskInfo` | `--enabled` 不等于 `false` 且未指定 `--no-wait` 时 | 轮询等待资源达到目标状态。（重复调用） |
| `CheckControlPlaneLogEnable` | 未指定 `--no-wait` 时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--cluster` | string | ✓ | ACK 集群 ID |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--aliuid` | string |  | 阿里云账号 ID |
| `--components` | string_array |  | 要采集的控制面组件，例如 apiserver,kcm,scheduler,ccm |
| `--enabled` | boolean |  | 开启或关闭控制面组件日志采集 |
| `--log-ttl` | integer |  | 日志保存天数 |
| `--project` | string |  | 控制面组件日志所在的 SLS Project |

## get

```bash
ecctl ack audit control-plane-log get [flags]
```

获取 ACK 托管集群控制面组件日志配置

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `CheckControlPlaneLogEnable` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--cluster` | string | ✓ | ACK 集群 ID |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | 要包含的资源字段，使用逗号分隔 |

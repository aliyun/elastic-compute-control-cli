---
generated: true
generated_by: "website/scripts/gen-reference.mjs"
generated_command: "make build && npm --prefix website run gen:reference"
title: ack trigger
sidebar_label: trigger
description: "管理 ACK 应用重新部署触发器"
---

# ack trigger

管理 ACK 应用重新部署触发器

运行 `ecctl ack trigger <action> -h` 查看用法，或 `ecctl schema ack.trigger.<action> --full` 获取该命令完整的结构化规格——每个参数与行为，便于 Agent 读取和填参。

## create

```bash
ecctl ack trigger create [flags]
```

创建触发器

- 类型：`mutation` · 风险：medium

| API | 调用时机 | 用途 |
|---|---|---|
| `CreateTrigger` | 每次执行命令时 | 执行资源操作。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--action` | string | ✓ | 触发器行为 |
| `--cluster` | string | ✓ | ACK 集群 ID |
| `--project` | string | ✓ | 触发器项目，格式为 namespace/name |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--type` | string |  | 触发器类型 |

## delete

```bash
ecctl ack trigger delete <id> [flags]
```

删除触发器

- 类型：`mutation` · 风险：high

| API | 调用时机 | 用途 |
|---|---|---|
| `DeleteTrigger` | 每次执行命令时 | 执行资源操作。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--cluster` | string | ✓ | ACK 集群 ID |
| `--region` | string | ✓ | Alibaba Cloud region |

## get

```bash
ecctl ack trigger get <id> [flags]
```

获取触发器详情

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `DescribeTrigger` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--cluster` | string | ✓ | ACK 集群 ID |
| `--name` | string | ✓ | 应用名称 |
| `--namespace` | string | ✓ | 应用命名空间 |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--action` | string |  | 触发器行为 |
| `--fields` | string |  | 要包含的资源字段，使用逗号分隔 |
| `--type` | string |  | 触发器类型 |

## list

```bash
ecctl ack trigger list [flags]
```

列出 ACK 应用触发器

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `DescribeTrigger` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--cluster` | string | ✓ | ACK 集群 ID |
| `--name` | string | ✓ | 应用名称 |
| `--namespace` | string | ✓ | 应用命名空间 |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--action` | string |  | 触发器行为 |
| `--fields` | string |  | 要包含的资源字段，使用逗号分隔 |
| `--filter` | key_value |  | 过滤表达式 key=value |
| `--type` | string |  | 触发器类型 |

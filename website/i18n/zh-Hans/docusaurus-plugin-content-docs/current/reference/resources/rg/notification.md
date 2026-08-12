---
generated: true
generated_by: "website/scripts/gen-reference.mjs"
generated_command: "make build && npm --prefix website run gen:reference"
title: rg notification
sidebar_label: notification
description: "管理资源组事件通知"
---

# rg notification

管理资源组事件通知

运行 `ecctl rg notification <action> -h` 查看用法，或 `ecctl schema rg.notification.<action> --full` 获取该命令完整的结构化规格——每个参数与行为，便于 Agent 读取和填参。

## get

```bash
ecctl rg notification get [flags]
```

获取资源组事件通知设置

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `GetResourceGroupNotificationSetting` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | 要包含的资源字段，使用逗号分隔 |

## disable

```bash
ecctl rg notification disable [flags]
```

禁用资源组事件通知

- 类型：`mutation` · 风险：medium

| API | 调用时机 | 用途 |
|---|---|---|
| `DisableResourceGroupNotification` | 每次执行命令时 | 执行资源操作。 |
| `GetResourceGroupNotificationSetting` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |

## enable

```bash
ecctl rg notification enable [flags]
```

启用资源组事件通知

- 类型：`mutation` · 风险：medium

| API | 调用时机 | 用途 |
|---|---|---|
| `EnableResourceGroupNotification` | 每次执行命令时 | 执行资源操作。 |
| `GetResourceGroupNotificationSetting` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |

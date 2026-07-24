---
generated: true
generated_by: "website/scripts/gen-reference.mjs"
generated_command: "make build && npm --prefix website run gen:reference"
title: rg resource
sidebar_label: resource
description: "管理资源组中的资源"
---

# rg resource

管理资源组中的资源

运行 `ecctl rg resource <action> -h` 查看用法，或 `ecctl schema rg.resource.<action> --full` 获取该命令完整的结构化规格——每个参数与行为，便于 Agent 读取和填参。

## update

```bash
ecctl rg resource update <id> [flags]
```

将资源移动到目标资源组

- 类型：`mutation` · 风险：medium
- 同步：等待 `matched`（waiter `resources_visible_after_move`，超时 `300s`）；用 `--no-wait` 跳过等待。

| API | 调用时机 | 用途 |
|---|---|---|
| `MoveResources` | 每次执行命令时 | 执行资源操作。 |
| `ListResources` | 未指定 `--no-wait` 时 | 轮询等待资源达到目标状态。（重复调用） |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--resource` | object | ✓ | 要移动的资源列表 |

## list

```bash
ecctl rg resource list <id> [flags]
```

列出资源组中的资源

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `ListResources` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | 要包含的资源字段，使用逗号分隔 |
| `--filter` | key_value |  | 过滤表达式 key=value |
| `--limit` | integer |  | 最多返回资源数量（默认：`100`） |
| `--page` | integer |  | 返回结果页码（默认：`1`） |

---
generated: true
generated_by: "website/scripts/gen-reference.mjs"
generated_command: "make build && npm --prefix website run gen:reference"
title: ack template
sidebar_label: template
description: "管理 ACK 编排模板"
---

# ack template

管理 ACK 编排模板

运行 `ecctl ack template <action> -h` 查看用法，或 `ecctl schema ack.template.<action> --full` 获取该命令完整的结构化规格——每个参数与行为，便于 Agent 读取和填参。

## create

```bash
ecctl ack template create [flags]
```

创建 ACK 编排模板

- 类型：`mutation` · 风险：medium

| API | 调用时机 | 用途 |
|---|---|---|
| `CreateTemplate` | 每次执行命令时 | 执行资源操作。 |
| `DescribeTemplateAttribute` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--content` | string | ✓ | 编排模板内容，JSON/YAML 文本或 @file |
| `--name` | string | ✓ | 编排模板名称 |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--description` | string |  | 编排模板描述 |
| `--tags` | string |  | 编排模板标签 |
| `--template-type` | string |  | 编排模板类型 |

## update

```bash
ecctl ack template update <id> [flags]
```

更新 ACK 编排模板

- 类型：`mutation` · 风险：medium

| API | 调用时机 | 用途 |
|---|---|---|
| `UpdateTemplate` | 每次执行命令时 | 执行资源操作。 |
| `DescribeTemplateAttribute` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--content` | string |  | 编排模板内容，JSON/YAML 文本或 @file |
| `--description` | string |  | 编排模板描述 |
| `--name` | string |  | 编排模板名称 |
| `--tags` | string |  | 编排模板标签 |
| `--template-type` | string |  | 编排模板类型 |

## delete

```bash
ecctl ack template delete <id> [flags]
```

删除 ACK 编排模板

- 类型：`mutation` · 风险：high
- 同步：等待 `absent`（waiter `deleted_after_delete`，超时 `300s`）；用 `--no-wait` 跳过等待。

| API | 调用时机 | 用途 |
|---|---|---|
| `DeleteTemplate` | 每次执行命令时 | 执行资源操作。 |
| `DescribeTemplateAttribute` | 未指定 `--no-wait` 时 | 轮询等待资源达到目标状态。（重复调用） |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |

## get

```bash
ecctl ack template get <id> [flags]
```

获取 ACK 编排模板

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `DescribeTemplateAttribute` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | 要包含的资源字段，使用逗号分隔 |
| `--template-type` | string |  | 编排模板类型 |

## list

```bash
ecctl ack template list [flags]
```

列出 ACK 编排模板

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `DescribeTemplates` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | 要包含的资源字段，使用逗号分隔 |
| `--limit` | integer |  | 最多返回资源数量（默认：`100`） |
| `--page` | integer |  | 返回结果页码（默认：`1`） |
| `--template-type` | string |  | 编排模板类型 |

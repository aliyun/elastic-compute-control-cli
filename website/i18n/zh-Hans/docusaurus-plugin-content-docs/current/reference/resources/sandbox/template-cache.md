---
generated: true
generated_by: "website/scripts/gen-reference.mjs"
generated_command: "make build && npm --prefix website run gen:reference"
title: sandbox template-cache
sidebar_label: template-cache
description: "管理原生 FC 沙箱模板镜像缓存。"
---

# sandbox template-cache

管理原生 FC 沙箱模板镜像缓存。

运行 `ecctl sandbox template-cache <action> -h` 查看用法，或 `ecctl schema sandbox.template-cache.<action> --full` 获取该命令完整的结构化规格——每个参数与行为，便于 Agent 读取和填参。

## create

```bash
ecctl sandbox template-cache create [flags]
```

提交原生 FC 模板缓存预热任务

- 类型：`mutation` · 风险：medium
- 同步：等待 `Success`（waiter `ready`，超时 `20m`）；用 `--no-wait` 跳过等待。

| API | 调用时机 | 用途 |
|---|---|---|
| `CreateTemplateCache` | 每次执行命令时 | 执行资源操作。 |
| `DescribeTemplateCache` | 未指定 `--no-wait` 时 | 轮询等待资源达到目标状态。（重复调用） |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--template-id` | string | ✓ | 已缓存模板 ID |
| `--team-id` | string |  | 所属团队 ID |

## delete

```bash
ecctl sandbox template-cache delete <id> [flags]
```

删除原生 FC 模板缓存

- 类型：`mutation` · 风险：high
- 同步：等待 `absent`（waiter `absent_after_delete`，超时 `300s`）；用 `--no-wait` 跳过等待。

| API | 调用时机 | 用途 |
|---|---|---|
| `DeleteTemplateCache` | 每次执行命令时 | 执行资源操作。 |
| `DescribeTemplateCache` | 未指定 `--no-wait` 时 | 轮询等待资源达到目标状态。（重复调用） |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--team-id` | string |  | 所属团队 ID |

## get

```bash
ecctl sandbox template-cache get <id> [flags]
```

获取原生 FC 模板缓存状态

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `DescribeTemplateCache` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | 要包含的资源字段，使用逗号分隔 |
| `--team-id` | string |  | 所属团队 ID |

## list

```bash
ecctl sandbox template-cache list [flags]
```

列出原生 FC 模板缓存

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `ListTemplateCache` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | 要包含的资源字段，使用逗号分隔 |
| `--limit` | integer |  | 每页最多返回的缓存数量（默认：`100`） |
| `--next-token` | string |  | 服务端分页令牌 |
| `--status` | string |  | 缓存状态，与模板构建状态不同；创建和查询返回的 creating 显示为 InProgress，服务端的 ready 显示为 Success |
| `--team-id` | string |  | 所属团队 ID |
| `--template-id` | string |  | 已缓存模板 ID |

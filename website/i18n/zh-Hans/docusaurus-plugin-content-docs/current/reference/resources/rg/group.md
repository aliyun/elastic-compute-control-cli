---
generated: true
generated_by: "website/scripts/gen-reference.mjs"
generated_command: "make build && npm --prefix website run gen:reference"
title: rg group
sidebar_label: group
description: "管理资源组"
---

# rg group

管理资源组

运行 `ecctl rg group <action> -h` 查看用法，或 `ecctl schema rg.group.<action> --full` 获取该命令完整的结构化规格——每个参数与行为，便于 Agent 读取和填参。

## create

```bash
ecctl rg group create [flags]
```

创建资源组

- 类型：`mutation` · 风险：medium
- 同步：等待 `OK`（waiter `ok_after_create`，超时 `10s`）；用 `--no-wait` 跳过等待。

| API | 调用时机 | 用途 |
|---|---|---|
| `CreateResourceGroup` | 每次执行命令时 | 执行资源操作。 |
| `GetResourceGroup` | 每次执行命令时 | 轮询等待资源达到目标状态。（重复调用） |
| `GetResourceGroup` | 每次执行命令时 | 返回最终资源视图。（复用等待结果，不额外请求） |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--display-name` | string | ✓ | 资源组显示名称 |
| `--name` | string | ✓ | 资源组唯一标识 |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--tag` | key_value |  | 标签赋值 key=value |

## update

```bash
ecctl rg group update <id> [flags]
```

更新资源组

- 类型：`mutation` · 风险：medium

| API | 调用时机 | 用途 |
|---|---|---|
| `UpdateResourceGroup` | 每次执行命令时 | 执行资源操作。 |
| `GetResourceGroup` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--display-name` | string |  | 资源组显示名称 |

## delete

```bash
ecctl rg group delete <id> [flags]
```

删除资源组

- 类型：`mutation` · 风险：high

| API | 调用时机 | 用途 |
|---|---|---|
| `DeleteResourceGroup` | 每次执行命令时 | 执行资源操作。 |
| `ListResourceGroups` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |

## get

```bash
ecctl rg group get <id> [flags]
```

获取资源组

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `GetResourceGroup` | 每次执行命令时 | 读取资源视图。 |
| `GetResourceGroupResourceCounts` | 指定 `--with-counts` 时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | 要包含的资源字段，使用逗号分隔 |
| `--with-counts` | boolean |  | 附带资源数量 |

## list

```bash
ecctl rg group list [<ids>...] [flags]
```

列出资源组

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `ListResourceGroups` | 未指定 `--with-auth-details` 时 | 读取资源视图。 |
| `ListResourceGroupsWithAuthDetails` | 指定 `--with-auth-details` 时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | 要包含的资源字段，使用逗号分隔 |
| `--filter` | key_value |  | 过滤表达式 key=value |
| `--limit` | integer |  | 最多返回资源数量（默认：`100`） |
| `--page` | integer |  | 返回结果页码（默认：`1`） |
| `--with-auth-details` | boolean |  | 附带授权信息 |

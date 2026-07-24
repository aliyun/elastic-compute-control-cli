---
generated: true
generated_by: "website/scripts/gen-reference.mjs"
generated_command: "make build && npm --prefix website run gen:reference"
title: rg role
sidebar_label: role
description: "管理 RAM 角色"
---

# rg role

管理 RAM 角色

运行 `ecctl rg role <action> -h` 查看用法，或 `ecctl schema rg.role.<action> --full` 获取该命令完整的结构化规格——每个参数与行为，便于 Agent 读取和填参。

## create

```bash
ecctl rg role create [flags]
```

创建角色

- 类型：`mutation` · 风险：medium

| API | 调用时机 | 用途 |
|---|---|---|
| `CreateRole` | 每次执行命令时 | 执行资源操作。 |
| `GetRole` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--assume-role-policy-document` | string | ✓ | 信任策略 JSON 或 @file |
| `--name` | string | ✓ | 角色名称 |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--description` | string |  | 角色描述 |
| `--max-session-duration` | integer |  | 最大会话持续时间（秒） |

## update

```bash
ecctl rg role update <name> [flags]
```

更新角色

- 类型：`mutation` · 风险：medium

| API | 调用时机 | 用途 |
|---|---|---|
| `UpdateRole` | 每次执行命令时 | 执行资源操作。 |
| `GetRole` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--assume-role-policy-document` | string |  | 信任策略 JSON 或 @file |
| `--description` | string |  | 角色描述 |
| `--max-session-duration` | integer |  | 最大会话持续时间（秒） |

## delete

```bash
ecctl rg role delete <name> [flags]
```

删除角色

- 类型：`mutation` · 风险：high

| API | 调用时机 | 用途 |
|---|---|---|
| `DeleteRole` | 每次执行命令时 | 执行资源操作。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |

## get

```bash
ecctl rg role get <name> [flags]
```

获取角色

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `GetRole` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | 要包含的资源字段，使用逗号分隔 |
| `--language` | string |  | 角色信息语言 |

## list

```bash
ecctl rg role list [flags]
```

列出角色

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `ListRoles` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | 要包含的资源字段，使用逗号分隔 |
| `--limit` | integer |  | 最多返回资源数量（默认：`100`） |
| `--page` | integer |  | 返回结果页码（默认：`1`） |

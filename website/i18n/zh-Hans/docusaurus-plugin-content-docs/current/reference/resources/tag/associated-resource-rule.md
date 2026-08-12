---
generated: true
generated_by: "website/scripts/gen-reference.mjs"
generated_command: "make build && npm --prefix website run gen:reference"
title: tag associated-resource-rule
sidebar_label: associated-resource-rule
description: "管理关联资源标签规则"
---

# tag associated-resource-rule

管理关联资源标签规则

运行 `ecctl tag associated-resource-rule <action> -h` 查看用法，或 `ecctl schema tag.associated-resource-rule.<action> --full` 获取该命令完整的结构化规格——每个参数与行为，便于 Agent 读取和填参。

## create

```bash
ecctl tag associated-resource-rule create [flags]
```

创建关联资源标签规则

- 类型：`mutation` · 风险：medium

| API | 调用时机 | 用途 |
|---|---|---|
| `CreateAssociatedResourceRules` | 每次执行命令时 | 执行资源操作。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--setting-name` | string | ✓ | 关联资源标签规则设置名称 |
| `--existing-status` | string |  | 是否对存量关联资源生效 |
| `--status` | string |  | 关联资源标签规则状态（默认：`Enable`） |
| `--tag-keys` | string_array |  | 关联资源标签规则作用的标签键列表 |

## update

```bash
ecctl tag associated-resource-rule update <setting-name> [flags]
```

更新关联资源标签规则

- 类型：`mutation` · 风险：medium

| API | 调用时机 | 用途 |
|---|---|---|
| `ListAssociatedResourceRules` | 指定 `&lt;setting-name>` 且（未指定 `--status` 或未指定 `--existing-status` 或未指定 `--tag-keys`）时 | 更新前读取现有规则，以保留未指定的字段。 |
| `UpdateAssociatedResourceRule` | 每次执行命令时 | 执行资源操作。 |
| `ListAssociatedResourceRules` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--existing-status` | string |  | 是否对存量关联资源生效 |
| `--status` | string |  | 关联资源标签规则状态 |
| `--tag-keys` | string_array |  | 关联资源标签规则作用的标签键列表 |

## delete

```bash
ecctl tag associated-resource-rule delete <setting-name> [flags]
```

删除关联资源标签规则

- 类型：`mutation` · 风险：high

| API | 调用时机 | 用途 |
|---|---|---|
| `DeleteAssociatedResourceRule` | 每次执行命令时 | 执行资源操作。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |

## list

```bash
ecctl tag associated-resource-rule list [<setting-name>...] [flags]
```

列出关联资源标签规则

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `ListAssociatedResourceRules` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | 要包含的资源字段，使用逗号分隔 |
| `--filter` | key_value |  | 过滤表达式 key=value |
| `--limit` | integer |  | 最多返回规则数量（默认：`100`） |
| `--next-token` | string |  | 下一页查询 Token |

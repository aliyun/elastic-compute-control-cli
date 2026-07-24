---
generated: true
generated_by: "website/scripts/gen-reference.mjs"
generated_command: "make build && npm --prefix website run gen:reference"
title: tag resource
sidebar_label: resource
description: "管理跨产品资源标签"
---

# tag resource

管理跨产品资源标签

运行 `ecctl tag resource <action> -h` 查看用法，或 `ecctl schema tag.resource.<action> --full` 获取该命令完整的结构化规格——每个参数与行为，便于 Agent 读取和填参。

## list

```bash
ecctl tag resource list [flags]
```

列出资源标签或按标签反查资源

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `ListResourcesByTag` | 指定 `--resource-type` 或指定 `--fuzzy-type` 或指定 `--include-all-tags` 时 | 读取资源视图。 |
| `ListTagResources` | 未指定 `--resource-type` 且未指定 `--fuzzy-type` 且未指定 `--include-all-tags` 时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | 要包含的资源字段，使用逗号分隔 |
| `--filter` | key_value |  | 过滤表达式 key=value |
| `--fuzzy-type` | string |  | 反查匹配模式 |
| `--include-all-tags` | boolean |  | 反查结果附带资源全部标签 |
| `--limit` | integer |  | 最多返回资源数量（默认：`100`） |
| `--next-token` | string |  | 下一页查询 token |
| `--resource-type` | string |  | 标签反查资源类型 |

## apply

```bash
ecctl tag resource apply [flags]
```

为资源绑定标签

- 类型：`mutation` · 风险：medium

| API | 调用时机 | 用途 |
|---|---|---|
| `TagResources` | 每次执行命令时 | 执行资源操作。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--arn` | string | ✓ | 资源 ARN |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--tag` | key_value | ✓ | 标签赋值或过滤 key=value |

## remove

```bash
ecctl tag resource remove [flags]
```

解绑资源标签

- 类型：`mutation` · 风险：high

| API | 调用时机 | 用途 |
|---|---|---|
| `UntagResources` | 每次执行命令时 | 执行资源操作。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--arn` | string | ✓ | 资源 ARN |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--tag-key` | string | ✓ | 要解绑的标签键 |

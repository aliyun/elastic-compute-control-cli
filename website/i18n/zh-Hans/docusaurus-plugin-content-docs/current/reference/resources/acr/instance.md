---
generated: true
generated_by: "website/scripts/gen-reference.mjs"
generated_command: "make build && npm --prefix website run gen:reference"
title: acr instance
sidebar_label: instance
description: "查询容器镜像服务企业版实例"
---

# acr instance

查询容器镜像服务企业版实例

运行 `ecctl acr instance <action> -h` 查看用法，或 `ecctl schema acr.instance.<action> --full` 获取该命令完整的结构化规格——每个参数与行为，便于 Agent 读取和填参。

## get

```bash
ecctl acr instance get <id> [flags]
```

获取企业版实例

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `GetInstance` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | 要包含的资源字段，使用逗号分隔 |

## list

```bash
ecctl acr instance list [flags]
```

列出企业版实例

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `ListInstance` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | 要包含的资源字段，使用逗号分隔 |
| `--filter` | key_value |  | 过滤表达式 key=value |
| `--limit` | integer |  | 最多返回实例数量（默认：`100`） |
| `--page` | integer |  | 返回结果页码（默认：`1`） |

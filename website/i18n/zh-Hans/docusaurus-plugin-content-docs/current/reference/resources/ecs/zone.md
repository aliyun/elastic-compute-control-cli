---
generated: true
generated_by: "website/scripts/gen-reference.mjs"
generated_command: "make build && npm --prefix website run gen:reference"
title: ecs zone
sidebar_label: zone
description: "查询地域内的 ECS 可用区"
---

# ecs zone

查询地域内的 ECS 可用区

运行 `ecctl ecs zone <action> -h` 查看用法，或 `ecctl schema ecs.zone.<action> --full` 获取该命令完整的结构化规格——每个参数与行为，便于 Agent 读取和填参。

## list

```bash
ecctl ecs zone list [flags]
```

列出可用区

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `DescribeZones` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--accept-language` | string |  | 可用区名称的展示语言 |
| `--fields` | string |  | 要包含的资源字段，使用逗号分隔 |
| `--filter` | key_value |  | 过滤表达式 key=value |
| `--verbose` | boolean |  | 返回每个可用区的完整可用资源详情 |

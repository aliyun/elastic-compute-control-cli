---
generated: true
generated_by: "website/scripts/gen-reference.mjs"
generated_command: "make build && npm --prefix website run gen:reference"
title: ack event
sidebar_label: event
description: "查询 ACK 控制面事件"
---

# ack event

查询 ACK 控制面事件

运行 `ecctl ack event <action> -h` 查看用法，或 `ecctl schema ack.event.<action> --full` 获取该命令完整的结构化规格——每个参数与行为，便于 Agent 读取和填参。

## list

```bash
ecctl ack event list [flags]
```

列出 ACK 事件

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `DescribeEventsForRegion` | 指定 `--by-region` 时 | 读取资源视图。 |
| `DescribeEvents` | 指定 `--type` 或指定 `--source` 时 | 读取资源视图。 |
| `DescribeClusterEvents` | 未指定 `--by-region` 且未指定 `--type` 且未指定 `--source` 时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--by-region` | boolean |  | 按当前地域跨集群列出事件 |
| `--cluster` | string |  | ACK 集群 ID |
| `--fields` | string |  | 要包含的资源字段，使用逗号分隔 |
| `--limit` | integer |  | 最多返回事件数量（默认：`100`） |
| `--page` | integer |  | 返回结果页码（默认：`1`） |
| `--source` | string |  | 事件来源 |
| `--type` | string |  | 事件类型 |

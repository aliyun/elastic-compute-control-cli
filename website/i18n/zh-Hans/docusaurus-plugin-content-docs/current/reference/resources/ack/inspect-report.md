---
generated: true
generated_by: "website/scripts/gen-reference.mjs"
generated_command: "make build && npm --prefix website run gen:reference"
title: ack inspect report
sidebar_label: inspect report
description: "触发和查询巡检报告"
---

# ack inspect report

触发和查询巡检报告

运行 `ecctl ack inspect report <action> -h` 查看用法，或 `ecctl schema ack.inspect.report.<action> --full` 获取该命令完整的结构化规格——每个参数与行为，便于 Agent 读取和填参。

## create

```bash
ecctl ack inspect report create [flags]
```

触发巡检报告

- 类型：`mutation` · 风险：medium

| API | 调用时机 | 用途 |
|---|---|---|
| `RunClusterInspect` | 每次执行命令时 | 执行资源操作。 |
| `ListClusterInspectReports` | 未指定 `--no-wait` 时 | 读取资源视图。 |
| `GetClusterInspectReportDetail` | 未指定 `--no-wait` 时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--cluster` | string | ✓ | ACK 集群 ID |
| `--region` | string | ✓ | Alibaba Cloud region |

## get

```bash
ecctl ack inspect report get <report-id> [flags]
```

查询巡检报告

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `GetClusterInspectReportDetail` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--cluster` | string | ✓ | ACK 集群 ID |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | 要包含的资源字段，使用逗号分隔 |

## list

```bash
ecctl ack inspect report list [flags]
```

列出巡检报告

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `ListClusterInspectReports` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--cluster` | string | ✓ | ACK 集群 ID |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | 要包含的资源字段，使用逗号分隔 |
| `--limit` | integer |  | 最多返回报告数量（默认：`50`） |
| `--next-token` | string |  | 上一次请求返回的分页令牌 |

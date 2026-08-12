---
generated: true
generated_by: "website/scripts/gen-reference.mjs"
generated_command: "make build && npm --prefix website run gen:reference"
title: ack policy
sidebar_label: policy
description: "管理 ACK 策略库条目"
---

# ack policy

管理 ACK 策略库条目

运行 `ecctl ack policy <action> -h` 查看用法，或 `ecctl schema ack.policy.<action> --full` 获取该命令完整的结构化规格——每个参数与行为，便于 Agent 读取和填参。

## get

```bash
ecctl ack policy get <name> [flags]
```

获取 ACK 策略库详情

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `DescribePolicyDetails` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | 要包含的资源字段，使用逗号分隔 |

## list

```bash
ecctl ack policy list [flags]
```

列出 ACK 策略库条目

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `DescribePolicies` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | 要包含的资源字段，使用逗号分隔 |
| `--limit` | integer |  | 最多返回资源数量（默认：`100`） |

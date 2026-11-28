---
generated: true
generated_by: "website/scripts/gen-reference.mjs"
generated_command: "make build && npm --prefix website run gen:reference"
title: sandbox quota
sidebar_label: quota
description: "管理原生 FC 沙箱团队的 CPU 和内存配额。"
---

# sandbox quota

管理原生 FC 沙箱团队的 CPU 和内存配额。

运行 `ecctl sandbox quota <action> -h` 查看用法，或 `ecctl schema sandbox.quota.<action> --full` 获取该命令完整的结构化规格——每个参数与行为，便于 Agent 读取和填参。

## update

```bash
ecctl sandbox quota update <id> [flags]
```

设置原生 FC 沙箱团队配额

- 类型：`mutation` · 风险：medium

| API | 调用时机 | 用途 |
|---|---|---|
| `UpdateQuota` | 每次执行命令时 | 执行资源操作。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--cpu-cores` | integer | ✓ | CPU 配额（核） |
| `--memory-gb` | integer | ✓ | 内存配额（GB） |
| `--region` | string | ✓ | Alibaba Cloud region |

## delete

```bash
ecctl sandbox quota delete <id> [flags]
```

删除原生 FC 沙箱团队配额配置

- 类型：`mutation` · 风险：high
- 同步：等待 `absent`（waiter `absent_after_delete`，超时 `300s`）；用 `--no-wait` 跳过等待。

| API | 调用时机 | 用途 |
|---|---|---|
| `DeleteQuota` | 每次执行命令时 | 执行资源操作。 |
| `DescribeQuota` | 每次执行命令时 | 轮询等待资源达到目标状态。（重复调用） |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |

## get

```bash
ecctl sandbox quota get <id> [flags]
```

获取原生 FC 沙箱团队配额

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `DescribeQuota` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | 要包含的资源字段，使用逗号分隔 |

## list

```bash
ecctl sandbox quota list [flags]
```

列出原生 FC 沙箱团队配额

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `ListQuota` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--all` | boolean |  | 从第一页获取所有令牌分页结果 |
| `--fields` | string |  | 要包含的资源字段，使用逗号分隔 |
| `--limit` | integer |  | 每页最多返回的配额数量（maxResults；CLI 上限 100）（默认：`100`） |
| `--next-token` | string |  | 服务端分页令牌 |

---
generated: true
generated_by: "website/scripts/gen-reference.mjs"
generated_command: "make build && npm --prefix website run gen:reference"
title: sandbox api-key
sidebar_label: api-key
description: "管理原生 FC 沙箱访问密钥。"
---

# sandbox api-key

管理原生 FC 沙箱访问密钥。

运行 `ecctl sandbox api-key <action> -h` 查看用法，或 `ecctl schema sandbox.api-key.<action> --full` 获取该命令完整的结构化规格——每个参数与行为，便于 Agent 读取和填参。

## create

```bash
ecctl sandbox api-key create [flags]
```

创建原生 FC 沙箱访问密钥

- 类型：`mutation` · 风险：medium

| API | 调用时机 | 用途 |
|---|---|---|
| `CreateApiKey` | 每次执行命令时 | 执行资源操作。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--name` | string | ✓ | 访问密钥名称 |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--expire-time` | string |  | 过期时间 |
| `--ip-blacklist` | object |  | IP 黑名单规则 |
| `--ip-whitelist` | object |  | IP 白名单规则 |
| `--show-value` | boolean |  | 创建或重置时展示访问密钥明文 |
| `--team-id` | string |  | 所属团队 ID |

## update

```bash
ecctl sandbox api-key update <id> [flags]
```

更新原生 FC 沙箱访问密钥

- 类型：`mutation` · 风险：medium

| API | 调用时机 | 用途 |
|---|---|---|
| `UpdateApiKey` | 每次执行命令时 | 执行资源操作。 |
| `DescribeApiKey` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--expire-time` | string |  | 过期时间 |
| `--ip-blacklist` | object |  | IP 黑名单规则 |
| `--ip-whitelist` | object |  | IP 白名单规则 |
| `--name` | string |  | 访问密钥名称 |
| `--status` | string |  | 访问密钥状态（active 或 inactive） |

## delete

```bash
ecctl sandbox api-key delete <id> [flags]
```

删除原生 FC 沙箱访问密钥

- 类型：`mutation` · 风险：high
- 同步：等待 `absent`（waiter `absent_after_delete`，超时 `300s`）；用 `--no-wait` 跳过等待。

| API | 调用时机 | 用途 |
|---|---|---|
| `DescribeApiKey` | 每次执行命令时 | 读取资源视图。 |
| `UpdateApiKey` | 前序步骤 `current_api_key` 的 `status` 字段等于 `active` 时 | 执行资源操作。 |
| `DeleteApiKey` | 每次执行命令时 | 执行资源操作。 |
| `DescribeApiKey` | 未指定 `--no-wait` 时 | 轮询等待资源达到目标状态。（重复调用） |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |

## get

```bash
ecctl sandbox api-key get <id> [flags]
```

获取原生 FC 沙箱访问密钥

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `DescribeApiKey` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | 要包含的资源字段，使用逗号分隔 |

## list

```bash
ecctl sandbox api-key list [flags]
```

列出原生 FC 沙箱访问密钥

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `ListApiKeys` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--all` | boolean |  | 从第一页获取所有结果 |
| `--fields` | string |  | 要包含的资源字段，使用逗号分隔 |
| `--limit` | integer |  | 每页最多返回的访问密钥数量（pageSize；CLI 上限 50）（默认：`50`） |
| `--name` | string |  | 访问密钥名称 |
| `--page` | integer |  | 返回结果页码（默认：`1`） |
| `--resource-group` | string |  | 资源组 ID |
| `--status` | string |  | 访问密钥状态（active 或 inactive） |
| `--team-id` | string |  | 所属团队 ID |
| `--user-id` | string |  | 创建者用户 ID |

## reset

```bash
ecctl sandbox api-key reset <id> [flags]
```

重置原生 FC 沙箱访问密钥

- 类型：`mutation` · 风险：medium

| API | 调用时机 | 用途 |
|---|---|---|
| `ResetApiKey` | 每次执行命令时 | 执行资源操作。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--show-value` | boolean |  | 创建或重置时展示访问密钥明文 |

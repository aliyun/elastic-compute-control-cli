---
generated: true
generated_by: "website/scripts/gen-reference.mjs"
generated_command: "make build && npm --prefix website run gen:reference"
title: rg policy
sidebar_label: policy
description: "管理权限策略"
---

# rg policy

管理权限策略

运行 `ecctl rg policy <action> -h` 查看用法，或 `ecctl schema rg.policy.<action> --full` 获取该命令完整的结构化规格——每个参数与行为，便于 Agent 读取和填参。

## create

```bash
ecctl rg policy create [flags]
```

创建权限策略

- 类型：`mutation` · 风险：medium

| API | 调用时机 | 用途 |
|---|---|---|
| `CreatePolicy` | 每次执行命令时 | 执行资源操作。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--name` | string | ✓ | 权限策略名称 |
| `--policy-document` | string | ✓ | 权限策略内容 JSON 或 @file |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--description` | string |  | 权限策略描述 |

## delete

```bash
ecctl rg policy delete <name> [flags]
```

删除权限策略

- 类型：`mutation` · 风险：high

| API | 调用时机 | 用途 |
|---|---|---|
| `DeletePolicy` | 每次执行命令时 | 执行资源操作。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |

## get

```bash
ecctl rg policy get <name> [flags]
```

获取权限策略

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `GetPolicy` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--policy-type` | string | ✓ | 权限策略类型 |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | 要包含的资源字段，使用逗号分隔 |
| `--language` | string |  | 策略描述语言 |

## list

```bash
ecctl rg policy list [flags]
```

列出权限策略

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `ListPolicies` | 未指定 `--resource-group` 且未指定 `--principal-type` 且未指定 `--principal-name` 时 | 读取资源视图。 |
| `ListPolicyAttachments` | 指定 `--resource-group` 或指定 `--principal-type` 或指定 `--principal-name` 时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | 要包含的资源字段，使用逗号分隔 |
| `--filter` | key_value |  | 过滤表达式 key=value |
| `--language` | string |  | 策略描述语言 |
| `--limit` | integer |  | 最多返回资源数量（默认：`100`） |
| `--page` | integer |  | 返回结果页码（默认：`1`） |
| `--principal-name` | string |  | 授权主体名称 |
| `--principal-type` | string |  | 授权主体类型 |
| `--resource-group` | string |  | 资源组 ID |

## attach

```bash
ecctl rg policy attach <name> [flags]
```

将权限策略绑定到授权主体

- 类型：`mutation` · 风险：medium
- 同步：等待 `present`（waiter `attached_after_attach`，超时 `300s`）；用 `--no-wait` 跳过等待。

| API | 调用时机 | 用途 |
|---|---|---|
| `AttachPolicy` | 每次执行命令时 | 执行资源操作。 |
| `ListPolicyAttachments` | 未指定 `--no-wait` 时 | 轮询等待资源达到目标状态。（重复调用） |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--policy-type` | string | ✓ | 权限策略类型 |
| `--principal-name` | string | ✓ | 授权主体名称 |
| `--principal-type` | string | ✓ | 授权主体类型 |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--resource-group` | string | ✓ | 资源组 ID |

## detach

```bash
ecctl rg policy detach <name> [flags]
```

解绑权限策略

- 类型：`mutation` · 风险：medium
- 同步：等待 `absent`（waiter `detached_after_detach`，超时 `300s`）；用 `--no-wait` 跳过等待。

| API | 调用时机 | 用途 |
|---|---|---|
| `DetachPolicy` | 每次执行命令时 | 执行资源操作。 |
| `ListPolicyAttachments` | 未指定 `--no-wait` 时 | 轮询等待资源达到目标状态。（重复调用） |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--policy-type` | string | ✓ | 权限策略类型 |
| `--principal-name` | string | ✓ | 授权主体名称 |
| `--principal-type` | string | ✓ | 授权主体类型 |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--resource-group` | string | ✓ | 资源组 ID |

---
generated: true
generated_by: "website/scripts/gen-reference.mjs"
generated_command: "make build && npm --prefix website run gen:reference"
title: tag policy
sidebar_label: policy
description: "管理标签策略"
---

# tag policy

管理标签策略

运行 `ecctl tag policy <action> -h` 查看用法，或 `ecctl schema tag.policy.<action> --full` 获取该命令完整的结构化规格——每个参数与行为，便于 Agent 读取和填参。

## create

```bash
ecctl tag policy create [flags]
```

创建标签策略

- 类型：`mutation` · 风险：medium
- 支持 `--dry-run` 校验。

| API | 调用时机 | 用途 |
|---|---|---|
| `CreatePolicy` | 每次执行命令时 | 执行资源操作。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--content` | string | ✓ | 标签策略内容 JSON 或 @file |
| `--name` | string | ✓ | 标签策略名称 |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--description` | string |  | 标签策略描述 |
| `--user-type` | string |  | 标签策略模式 |

## update

```bash
ecctl tag policy update <id> [flags]
```

更新标签策略

- 类型：`mutation` · 风险：medium
- 支持 `--dry-run` 校验。

| API | 调用时机 | 用途 |
|---|---|---|
| `ModifyPolicy` | 每次执行命令时 | 执行资源操作。 |
| `GetPolicy` | 未指定 `--no-wait` 且未指定 `--dry-run` 时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--content` | string |  | 标签策略内容 JSON 或 @file |
| `--description` | string |  | 标签策略描述 |
| `--name` | string |  | 标签策略名称 |

## delete

```bash
ecctl tag policy delete <id> [flags]
```

删除标签策略

- 类型：`mutation` · 风险：high

| API | 调用时机 | 用途 |
|---|---|---|
| `DeletePolicy` | 每次执行命令时 | 执行资源操作。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |

## get

```bash
ecctl tag policy get <id> [flags]
```

获取标签策略

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `GetPolicy` | 每次执行命令时 | 读取资源视图。 |
| `GetPolicyEnableStatus` | 指定 `--with-status` 时 | 读取资源视图。 |
| `GetEffectivePolicy` | 指定 `--with-effective` 时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | 要包含的资源字段，使用逗号分隔 |
| `--open-type` | string |  | 策略状态开通类型 |
| `--tag-keys` | array |  | 有效策略查询的标签键 |
| `--target` | string |  | 目标节点 ID |
| `--target-type` | string |  | 目标节点类型 |
| `--user-type` | string |  | 标签策略模式 |
| `--with-effective` | boolean |  | 附带目标节点的有效策略 |
| `--with-status` | boolean |  | 附带标签策略启用状态 |

## list

```bash
ecctl tag policy list [<ids>...] [flags]
```

列出标签策略

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `ListPolicies` | 未指定 `--target` 且未指定 `--target-type` 且未指定 `--targets-for-policy` 时 | 读取资源视图。 |
| `ListPoliciesForTarget` | 指定 `--target` 或指定 `--target-type` 时 | 读取资源视图。 |
| `ListTargetsForPolicy` | 指定 `--targets-for-policy` 时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | 要包含的资源字段，使用逗号分隔 |
| `--filter` | key_value |  | 过滤表达式 key=value |
| `--limit` | integer |  | 最多返回资源数量（默认：`100`） |
| `--next-token` | string |  | 下一页查询 token |
| `--target` | string |  | 目标节点 ID |
| `--target-type` | string |  | 目标节点类型 |
| `--targets-for-policy` | string |  | 查询绑定目标节点的标签策略 ID |

## attach

```bash
ecctl tag policy attach <id> [flags]
```

将标签策略绑定到目标节点

- 类型：`mutation` · 风险：medium
- 同步：等待 `present`（waiter `attached_after_attach`，超时 `300s`）；用 `--no-wait` 跳过等待。

| API | 调用时机 | 用途 |
|---|---|---|
| `AttachPolicy` | 每次执行命令时 | 执行资源操作。 |
| `ListPoliciesForTarget` | 未指定 `--no-wait` 时 | 轮询等待资源达到目标状态。（重复调用） |
| `ListPoliciesForTarget` | 未指定 `--no-wait` 时 | 返回最终资源视图。（复用等待结果，不额外请求） |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--target` | string |  | 目标节点 ID |
| `--target-type` | string |  | 目标节点类型 |

## detach

```bash
ecctl tag policy detach <id> [flags]
```

解绑标签策略

- 类型：`mutation` · 风险：medium
- 同步：等待 `absent`（waiter `detached_after_detach`，超时 `300s`）；用 `--no-wait` 跳过等待。

| API | 调用时机 | 用途 |
|---|---|---|
| `DetachPolicy` | 每次执行命令时 | 执行资源操作。 |
| `ListPoliciesForTarget` | 未指定 `--no-wait` 时 | 轮询等待资源达到目标状态。（重复调用） |
| `ListPoliciesForTarget` | 未指定 `--no-wait` 时 | 返回最终资源视图。（复用等待结果，不额外请求） |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--target` | string |  | 目标节点 ID |
| `--target-type` | string |  | 目标节点类型 |

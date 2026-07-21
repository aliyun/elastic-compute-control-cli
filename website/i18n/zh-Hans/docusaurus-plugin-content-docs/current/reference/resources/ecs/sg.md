---
generated: true
generated_by: "website/scripts/gen-reference.mjs"
generated_command: "make build && npm --prefix website run gen:reference"
title: ecs sg
sidebar_label: sg
description: "管理安全组资源"
---

# ecs sg

管理安全组资源

运行 `ecctl ecs sg <action> -h` 查看用法，或 `ecctl schema ecs.sg.<action> --full` 获取该命令完整的结构化规格——每个参数与行为，便于 Agent 读取和填参。

## create

```bash
ecctl ecs sg create [flags]
```

创建安全组

- 类型：`mutation` · 风险：medium
- 通过 `ClientToken` 幂等。

| API | 调用时机 | 用途 |
|---|---|---|
| `CreateSecurityGroup` | 每次执行命令时 | 执行资源操作。 |
| `DescribeSecurityGroupAttribute` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--vpc` | string | ✓ | VPC ID |
| `--description` | string |  | 安全组描述 |
| `--name` | string |  | 安全组名称 |
| `--resource-group` | string |  | 资源组 ID |
| `--tag` | key_value |  | 标签赋值 key=value |
| `--type` | string |  | 安全组类型（默认：`normal`） |

## update

```bash
ecctl ecs sg update <id> [flags]
```

更新安全组

- 类型：`mutation` · 风险：medium

| API | 调用时机 | 用途 |
|---|---|---|
| `ModifySecurityGroupAttribute` | 指定 `--name` 或指定 `--description` 时 | 执行资源操作。 |
| `ModifySecurityGroupPolicy` | 指定 `--inner-access-policy` 时 | 执行资源操作。 |
| `ModifySecurityGroupRule` | （指定 `--rule-id` 或指定 `--protocol` 或指定 `--port` 或指定 `--cidr` 或指定 `--policy` 或指定 `--priority`）且 `--direction` 不等于 `egress` 时 | 执行资源操作。 |
| `ModifySecurityGroupEgressRule` | `--direction` 等于 `egress` 且（指定 `--rule-id` 或指定 `--protocol` 或指定 `--port` 或指定 `--cidr` 或指定 `--policy` 或指定 `--priority`）时 | 执行资源操作。 |
| `DescribeSecurityGroupAttribute` | 未指定 `--no-wait` 时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--cidr` | cidr |  | 规则 IPv4 CIDR 网段 |
| `--description` | string |  | 安全组描述 |
| `--direction` | string |  | 规则方向 |
| `--inner-access-policy` | string |  | 组内连通策略 |
| `--name` | string |  | 安全组名称 |
| `--policy` | string |  | 规则策略 |
| `--port` | string |  | 规则端口或端口范围 |
| `--priority` | integer |  | 规则优先级 |
| `--protocol` | string |  | 规则协议 |
| `--rule-id` | string |  | 安全组规则 ID |

## delete

```bash
ecctl ecs sg delete <id> [flags]
```

删除安全组

- 类型：`mutation` · 风险：high

| API | 调用时机 | 用途 |
|---|---|---|
| `DeleteSecurityGroup` | 每次执行命令时 | 执行资源操作。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |

## get

```bash
ecctl ecs sg get <id> [flags]
```

获取安全组

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `DescribeSecurityGroupAttribute` | 每次执行命令时 | 读取资源视图。 |
| `DescribeSecurityGroupReferences` | 指定 `--with-references` 时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | 要包含的资源字段，使用逗号分隔 |
| `--with-references` | boolean |  | 附带安全组引用关系 |

## list

```bash
ecctl ecs sg list [<ids>...] [flags]
```

列出安全组

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `DescribeSecurityGroups` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | 要包含的资源字段，使用逗号分隔 |
| `--filter` | key_value |  | 过滤表达式 key=value |
| `--limit` | integer |  | 最多返回资源数量（默认：`100`） |
| `--next-token` | string |  | 下一页查询 token |

## authorize

```bash
ecctl ecs sg authorize <id> [flags]
```

授权安全组规则

- 类型：`mutation` · 风险：medium
- 通过 `ClientToken` 幂等。

| API | 调用时机 | 用途 |
|---|---|---|
| `AuthorizeSecurityGroup` | `--direction` 不等于 `egress` 时 | 执行资源操作。 |
| `AuthorizeSecurityGroupEgress` | `--direction` 等于 `egress` 时 | 执行资源操作。 |
| `DescribeSecurityGroupAttribute` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--cidr` | cidr |  | 规则 IPv4 CIDR 网段 |
| `--direction` | string |  | 规则方向（默认：`ingress`） |
| `--policy` | string |  | 规则策略（默认：`accept`） |
| `--port` | string |  | 规则端口或端口范围 |
| `--priority` | integer |  | 规则优先级（默认：`1`） |
| `--protocol` | string |  | 规则协议 |
| `--rule` | string |  | 安全组规则，例如 ingress:tcp:80:0.0.0.0/0 或 tcp:80@0.0.0.0/0 |

## revoke

```bash
ecctl ecs sg revoke <id> [flags]
```

撤销安全组规则

- 类型：`mutation` · 风险：medium
- 通过 `ClientToken` 幂等。

| API | 调用时机 | 用途 |
|---|---|---|
| `RevokeSecurityGroup` | `--direction` 不等于 `egress` 时 | 执行资源操作。 |
| `RevokeSecurityGroupEgress` | `--direction` 等于 `egress` 时 | 执行资源操作。 |
| `DescribeSecurityGroupAttribute` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--direction` | string |  | 规则方向（默认：`ingress`） |
| `--rule` | string |  | 安全组规则，例如 ingress:tcp:80:0.0.0.0/0 或 tcp:80@0.0.0.0/0 |
| `--rule-id` | string |  | 安全组规则 ID |

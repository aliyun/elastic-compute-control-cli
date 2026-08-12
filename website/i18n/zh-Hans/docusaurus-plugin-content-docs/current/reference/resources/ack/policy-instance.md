---
generated: true
generated_by: "website/scripts/gen-reference.mjs"
generated_command: "make build && npm --prefix website run gen:reference"
title: ack policy instance
sidebar_label: policy instance
description: "管理集群中的 ACK 策略实例"
---

# ack policy instance

管理集群中的 ACK 策略实例

运行 `ecctl ack policy instance <action> -h` 查看用法，或 `ecctl schema ack.policy.instance.<action> --full` 获取该命令完整的结构化规格——每个参数与行为，便于 Agent 读取和填参。

## create

```bash
ecctl ack policy instance create <policy-name> [flags]
```

创建 ACK 策略实例

- 类型：`mutation` · 风险：medium

| API | 调用时机 | 用途 |
|---|---|---|
| `DeployPolicyInstance` | 每次执行命令时 | 执行资源操作。 |
| `DescribePolicyInstances` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--action` | string | ✓ | 策略动作 |
| `--cluster` | string | ✓ | ACK 集群 ID |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--namespaces` | array |  | 策略生效命名空间 JSON 数组 |
| `--parameters` | object |  | 策略实例参数 JSON 对象或 @file |

## update

```bash
ecctl ack policy instance update <policy-name> [flags]
```

更新 ACK 策略实例

- 类型：`mutation` · 风险：medium

| API | 调用时机 | 用途 |
|---|---|---|
| `ModifyPolicyInstance` | 每次执行命令时 | 执行资源操作。 |
| `DescribePolicyInstances` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--cluster` | string | ✓ | ACK 集群 ID |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--action` | string |  | 策略动作 |
| `--instance-name` | string |  | 策略实例名称 |
| `--namespaces` | array |  | 策略生效命名空间 JSON 数组 |
| `--parameters` | object |  | 策略实例参数 JSON 对象或 @file |

## delete

```bash
ecctl ack policy instance delete <policy-name> [flags]
```

删除 ACK 策略实例

- 类型：`mutation` · 风险：high

| API | 调用时机 | 用途 |
|---|---|---|
| `DeletePolicyInstance` | 每次执行命令时 | 执行资源操作。 |
| `DescribePolicyInstances` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--cluster` | string | ✓ | ACK 集群 ID |
| `--instance-name` | string | ✓ | 策略实例名称 |
| `--region` | string | ✓ | Alibaba Cloud region |

## get

```bash
ecctl ack policy instance get <policy-name> [flags]
```

获取 ACK 策略实例

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `DescribePolicyInstances` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--cluster` | string | ✓ | ACK 集群 ID |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | 要包含的资源字段，使用逗号分隔 |
| `--instance-name` | string |  | 策略实例名称 |

## list

```bash
ecctl ack policy instance list [flags]
```

列出集群中的 ACK 策略实例

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `DescribePolicyInstances` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--cluster` | string | ✓ | ACK 集群 ID |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | 要包含的资源字段，使用逗号分隔 |
| `--instance-name` | string |  | 策略实例名称 |
| `--policy-name` | string |  | 策略名称 |

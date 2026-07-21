---
generated: true
generated_by: "website/scripts/gen-reference.mjs"
generated_command: "make build && npm --prefix website run gen:reference"
title: ack
sidebar_label: ack
description: "管理 ACK 集群"
---

# ack

管理 ACK 集群

运行 `ecctl ack <action> -h` 查看用法，或 `ecctl schema ack.ack.<action> --full` 获取该命令完整的结构化规格——每个参数与行为，便于 Agent 读取和填参。

## create

```bash
ecctl ack create [flags]
```

创建 ACK 集群

- 类型：`mutation` · 风险：medium
- 同步：等待 `running`（waiter `running_after_create`，超时 `1800s`）；用 `--no-wait` 跳过等待。

| API | 调用时机 | 用途 |
|---|---|---|
| `CreateCluster` | 每次执行命令时 | 执行资源操作。 |
| `DescribeClusterDetail` | 未指定 `--no-wait` 时 | 轮询等待资源达到目标状态。（重复调用） |
| `DescribeClusterDetail` | 未指定 `--no-wait` 时 | 返回最终资源视图。（复用等待结果，不额外请求） |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--name` | string | ✓ | 集群名称 |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--type` | string | ✓ | 集群类型 |
| `--api-server-public` | boolean |  | 通过公网端点开放 API Server |
| `--edition` | string |  | 集群版本规格；常见值包括 ack.standard、ack.pro.small、ack.pro.xlarge、ack.pro.2xlarge、ack.pro.4xlarge |
| `--pod-cidr` | cidr |  | Pod 网络 CIDR，作为 ACK CreateCluster 的 container_cidr 发送 |
| `--profile` | string |  | ACK 集群场景类型。如需同时指定配置 profile 和 ACK profile，请将配置 profile 放在资源命令前，例如 ecctl --profile prod ack create --profile Serverless。 |
| `--resource-group` | string |  | 资源组 ID |
| `--service-cidr` | cidr |  | Service 网络 CIDR |
| `--snat-entry` | boolean |  | 在 ACK 支持时创建用于集群出站访问的 SNAT 条目 |
| `--tag` | key_value |  | 标签赋值 key=value |
| `--vpc` | string |  | VPC ID |
| `--vswitch` | string_array |  | 集群网络使用的交换机 ID |
| `--zone` | string_array |  | ACK CreateCluster zone_ids 使用的可用区 ID |

## update

```bash
ecctl ack update <id> [flags]
```

更新 ACK 集群

- 类型：`mutation` · 风险：medium
- 同步：等待 `running`（waiter `running_after_update`，超时 `1800s`）；用 `--no-wait` 跳过等待。

| API | 调用时机 | 用途 |
|---|---|---|
| `ModifyCluster` | 指定 `--name` 或指定 `--api-server-public` 或指定 `--api-server-eip-id` 或指定 `--resource-group` 或指定 `--maintenance-window` 时 | 执行资源操作。 |
| `DescribeClusterDetail` | （指定 `--name` 或指定 `--api-server-public` 或指定 `--api-server-eip-id` 或指定 `--resource-group` 或指定 `--maintenance-window`）且未指定 `--no-wait` 时 | 轮询等待资源达到目标状态。（重复调用） |
| `MigrateCluster` | 指定 `--to-edition` 时 | 执行资源操作。 |
| `DescribeClusterDetail` | 指定 `--to-edition` 且未指定 `--no-wait` 时 | 轮询等待资源达到目标状态。（重复调用） |
| `ModifyClusterTags` | 指定 `--tag-replace` 时 | 执行资源操作。 |
| `TagResources` | 指定 `--tag` 时 | 执行资源操作。 |
| `UntagResources` | 指定 `--remove-tag` 时 | 执行资源操作。 |
| `DescribeClusterDetail` | 未指定 `--no-wait` 时 | 读取资源视图。 |
| `ListTagResources` | 未指定 `--no-wait` 时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--api-server-eip-id` | string |  | 开放 API Server 公网访问时绑定的 EIP ID |
| `--api-server-public` | boolean |  | 通过公网端点开放 API Server |
| `--maintenance-window` | object |  | 维护窗口，使用 ACK 字段名的 JSON 或 @file |
| `--name` | string |  | 集群名称 |
| `--remove-tag` | string_array |  | 要移除的标签键 |
| `--resource-group` | string |  | 资源组 ID |
| `--tag` | key_value |  | 标签赋值 key=value |
| `--tag-replace` | key_value |  | 全量替换标签集合 key=value |
| `--to-edition` | string |  | 迁移目标集群版本规格 |

## delete

```bash
ecctl ack delete <id> [flags]
```

删除 ACK 集群

- 类型：`mutation` · 风险：high
- 同步：等待 `absent`（waiter `deleted_after_delete`，超时 `1800s`）；用 `--no-wait` 跳过等待。

| API | 调用时机 | 用途 |
|---|---|---|
| `DeleteCluster` | 每次执行命令时 | 执行资源操作。 |
| `DescribeClustersV1` | 未指定 `--no-wait` 时 | 轮询等待资源达到目标状态。（重复调用） |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--delete-options` | object |  | ACK delete_options JSON 或 @file |
| `--force` | boolean |  | 删除集群关联资源而不是保留它们（默认：`false`） |
| `--retain-all-resources` | boolean |  | 删除集群时保留全部云资源 |
| `--retain-resources` | string_array |  | 删除时保留的云资源 ID |

## get

```bash
ecctl ack get <id> [flags]
```

获取 ACK 集群

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `DescribeClusterDetail` | 每次执行命令时 | 读取资源视图。 |
| `DescribeClusterResources` | 指定 `--with-resources` 时 | 读取资源视图。 |
| `ListTagResources` | 指定 `--with-tags` 时 | 读取资源视图。 |
| `DescribePolicyGovernanceInCluster` | 指定 `--with-policy-governance` 时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | 要包含的资源字段，使用逗号分隔 |
| `--with-policy-governance` | boolean |  | 附带策略治理概览 |
| `--with-resources` | boolean |  | 附带集群关联云资源 |
| `--with-tags` | boolean |  | 附带集群标签 |

## list

```bash
ecctl ack list [<ids>...] [flags]
```

列出 ACK 集群

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `DescribeClustersForRegion` | 指定 `--cross-account` 时 | 读取资源视图。 |
| `DescribeClustersV1` | 未指定 `--cross-account` 时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--cross-account` | boolean |  | 使用地域维度跨账号集群列表 API |
| `--fields` | string |  | 要包含的资源字段，使用逗号分隔 |
| `--filter` | key_value |  | 过滤表达式 key=value |
| `--limit` | integer |  | 最多返回集群数量（默认：`100`） |
| `--page` | integer |  | 返回结果页码（默认：`1`） |

## upgrade

```bash
ecctl ack upgrade <id> [flags]
```

升级 ACK 集群

- 类型：`mutation` · 风险：medium
- 同步：等待 `running`（waiter `running_after_upgrade`，超时 `3600s`）；用 `--no-wait` 跳过等待。

| API | 调用时机 | 用途 |
|---|---|---|
| `UpgradeCluster` | 每次执行命令时 | 执行资源操作。 |
| `DescribeClusterDetail` | 未指定 `--no-wait` 时 | 轮询等待资源达到目标状态。（重复调用） |
| `DescribeClusterDetail` | 未指定 `--no-wait` 时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--version` | string | ✓ | 目标 Kubernetes 版本 |
| `--master-only` | boolean |  | 仅升级控制面节点 |
| `--max-parallelism` | integer |  | 最大并行升级节点数 |
| `--rolling-policy` | object |  | 升级滚动策略，使用 ACK 字段名的 JSON 或 @file |

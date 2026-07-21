---
generated: true
generated_by: "website/scripts/gen-reference.mjs"
generated_command: "make build && npm --prefix website run gen:reference"
title: ack nodepool
sidebar_label: nodepool
description: "管理 ACK 节点池资源"
---

# ack nodepool

管理 ACK 节点池资源

运行 `ecctl ack nodepool <action> -h` 查看用法，或 `ecctl schema ack.nodepool.<action> --full` 获取该命令完整的结构化规格——每个参数与行为，便于 Agent 读取和填参。

## create

```bash
ecctl ack nodepool create [flags]
```

创建节点池

- 类型：`mutation` · 风险：medium
- 同步：等待 `active`（waiter `active_after_change`，超时 `600s`）；用 `--no-wait` 跳过等待。

| API | 调用时机 | 用途 |
|---|---|---|
| `CreateClusterNodePool` | 指定 `--config` 时 | 执行资源操作。 |
| `DescribeClusterNodePoolDetail` | 指定 `--config` 且未指定 `--no-wait` 时 | 轮询等待资源达到目标状态。（重复调用） |
| `CreateClusterNodePool` | 未指定 `--config` 时 | 执行资源操作。 |
| `DescribeClusterNodePoolDetail` | 未指定 `--config` 且未指定 `--no-wait` 时 | 轮询等待资源达到目标状态。（重复调用） |
| `DescribeClusterNodePoolDetail` | 未指定 `--no-wait` 时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--cluster` | string | ✓ | ACK 集群 ID |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--config` | object |  | 节点池请求体，JSON 对象或 @file |
| `--desired-size` | integer |  | 节点池期望节点数 |
| `--instance-type` | string_array |  | 节点池伸缩组使用的 ECS 实例规格 |
| `--internet-max-bandwidth-out` | integer |  | 节点池实例公网出方向最大带宽，单位 Mbit/s |
| `--name` | string |  | 节点池名称 |
| `--runtime` | string |  | 节点池节点容器运行时 |
| `--runtime-version` | string |  | 节点池节点容器运行时版本 |
| `--system-disk-category` | string |  | 节点池实例系统盘类型 |
| `--system-disk-size` | integer |  | 节点池实例系统盘大小，单位 GiB |
| `--vswitch` | string_array |  | 节点池伸缩组使用的交换机 ID |

## update

```bash
ecctl ack nodepool update <id> [flags]
```

更新节点池

- 类型：`mutation` · 风险：medium
- 同步：等待 `active`（waiter `active_after_change`，超时 `600s`）；用 `--no-wait` 跳过等待。

| API | 调用时机 | 用途 |
|---|---|---|
| `ModifyClusterNodePool` | 指定 `--config` 时 | 执行资源操作。 |
| `DescribeClusterNodePoolDetail` | 指定 `--config` 且未指定 `--no-wait` 时 | 轮询等待资源达到目标状态。（重复调用） |
| `ScaleClusterNodePool` | 指定 `--desired-size` 时 | 执行资源操作。 |
| `DescribeClusterNodePoolDetail` | 指定 `--desired-size` 且未指定 `--no-wait` 时 | 轮询等待资源达到目标状态。（重复调用） |
| `ModifyNodePoolNodeConfig` | 指定 `--with-node-config` 时 | 执行资源操作。 |
| `DescribeClusterNodePoolDetail` | 指定 `--with-node-config` 且未指定 `--no-wait` 时 | 轮询等待资源达到目标状态。（重复调用） |
| `TagResources` | 指定 `--tag` 时 | 执行资源操作。 |
| `UntagResources` | 指定 `--untag` 时 | 执行资源操作。 |
| `DescribeClusterNodePoolDetail` | 未指定 `--no-wait` 时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--cluster` | string | ✓ | ACK 集群 ID |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--config` | object |  | 节点池请求体，JSON 对象或 @file |
| `--desired-size` | integer |  | 节点池期望节点数 |
| `--node-config` | object |  | 节点级配置请求体，JSON 对象或 @file |
| `--tag` | key_value |  | 阿里云资源标签赋值 key=value |
| `--untag` | string_array |  | 要移除的阿里云资源标签键 |
| `--with-node-config` | boolean |  | 将 update 路由到节点级配置 |

## delete

```bash
ecctl ack nodepool delete <id> [flags]
```

删除节点池

- 类型：`mutation` · 风险：high
- 同步：等待 `absent`（waiter `absent_after_delete`，超时 `600s`）；用 `--no-wait` 跳过等待。

| API | 调用时机 | 用途 |
|---|---|---|
| `DeleteClusterNodepool` | 每次执行命令时 | 执行资源操作。 |
| `DescribeClusterNodePools` | 未指定 `--no-wait` 时 | 轮询等待资源达到目标状态。（重复调用） |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--cluster` | string | ✓ | ACK 集群 ID |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--force` | boolean |  | 在 API 支持时启用强制删除语义（默认：`false`） |

## get

```bash
ecctl ack nodepool get <id> [flags]
```

获取节点池

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `DescribeClusterNodePoolDetail` | 每次执行命令时 | 读取资源视图。 |
| `DescribeNodePoolVuls` | 指定 `--with-vuls` 时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--cluster` | string | ✓ | ACK 集群 ID |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | 要包含的资源字段，使用逗号分隔 |
| `--necessity` | string |  | 漏洞修复必要性过滤 |
| `--with-vuls` | boolean |  | 附带节点池漏洞详情 |

## list

```bash
ecctl ack nodepool list [flags]
```

列出节点池

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `DescribeClusterNodePools` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--cluster` | string | ✓ | ACK 集群 ID |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | 要包含的资源字段，使用逗号分隔 |
| `--name` | string |  | 节点池名称 |

## attach

```bash
ecctl ack nodepool attach <id> [flags]
```

将实例加入节点池

- 类型：`mutation` · 风险：medium
- 同步：等待 `active`（waiter `active_after_change`，超时 `600s`）；用 `--no-wait` 跳过等待。

| API | 调用时机 | 用途 |
|---|---|---|
| `AttachInstancesToNodePool` | 未指定 `--print-script-only` 时 | 执行资源操作。 |
| `DescribeClusterNodePoolDetail` | 未指定 `--print-script-only` 且未指定 `--no-wait` 时 | 轮询等待资源达到目标状态。（重复调用） |
| `DescribeClusterAttachScripts` | 指定 `--print-script-only` 时 | 读取资源视图。 |
| `DescribeClusterNodePoolDetail` | 未指定 `--no-wait` 且未指定 `--print-script-only` 时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--cluster` | string | ✓ | ACK 集群 ID |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--config` | object |  | 节点池请求体，JSON 对象或 @file |
| `--instance` | string_array |  | ECS 实例 ID |
| `--print-script-only` | boolean |  | 仅打印加入脚本，不执行实例加入 |

## detach

```bash
ecctl ack nodepool detach <id> [flags]
```

从节点池移除节点

- 类型：`mutation` · 风险：medium
- 同步：等待 `active`（waiter `active_after_change`，超时 `600s`）；用 `--no-wait` 跳过等待。

| API | 调用时机 | 用途 |
|---|---|---|
| `RemoveNodePoolNodes` | 每次执行命令时 | 执行资源操作。 |
| `DescribeClusterNodePoolDetail` | 未指定 `--no-wait` 时 | 轮询等待资源达到目标状态。（重复调用） |
| `DescribeClusterNodePoolDetail` | 未指定 `--no-wait` 时 | 返回最终资源视图。（复用等待结果，不额外请求） |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--cluster` | string | ✓ | ACK 集群 ID |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--concurrency` | boolean |  | 在支持时并发移除节点 |
| `--drain-node` | boolean |  | 移除前排空节点 |
| `--force` | boolean |  | 在 API 支持时启用强制删除语义（默认：`false`） |
| `--instance` | string_array |  | ECS 实例 ID |
| `--node` | string_array |  | Kubernetes 节点名称或 ID |

## repair

```bash
ecctl ack nodepool repair <id> [flags]
```

修复节点池

- 类型：`mutation` · 风险：medium
- 同步：等待 `active`（waiter `active_after_change`，超时 `600s`）；用 `--no-wait` 跳过等待。

| API | 调用时机 | 用途 |
|---|---|---|
| `RepairClusterNodePool` | 指定 `--node` 或指定 `--config` 或指定 `--api-param` 时 | 执行资源操作。 |
| `DescribeClusterNodePoolDetail` | （指定 `--node` 或指定 `--config` 或指定 `--api-param`）且未指定 `--no-wait` 时 | 轮询等待资源达到目标状态。（重复调用） |
| `FixNodePoolVuls` | 指定 `--vulnerabilities` 时 | 执行资源操作。 |
| `DescribeClusterNodePoolDetail` | 指定 `--vulnerabilities` 且未指定 `--no-wait` 时 | 轮询等待资源达到目标状态。（重复调用） |
| `DescribeClusterNodePoolDetail` | 未指定 `--no-wait` 时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--cluster` | string | ✓ | ACK 集群 ID |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--config` | object |  | 节点池请求体，JSON 对象或 @file |
| `--node` | string_array |  | Kubernetes 节点名称或 ID |
| `--vulnerabilities` | string_array |  | 要修复的漏洞 ID |

## upgrade

```bash
ecctl ack nodepool upgrade <id> [flags]
```

升级节点池

- 类型：`mutation` · 风险：medium
- 同步：等待 `active`（waiter `active_after_change`，超时 `600s`）；用 `--no-wait` 跳过等待。

| API | 调用时机 | 用途 |
|---|---|---|
| `UpgradeClusterNodepool` | 每次执行命令时 | 执行资源操作。 |
| `DescribeClusterNodePoolDetail` | 未指定 `--no-wait` 时 | 轮询等待资源达到目标状态。（重复调用） |
| `DescribeClusterNodePoolDetail` | 未指定 `--no-wait` 时 | 返回最终资源视图。（复用等待结果，不额外请求） |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--cluster` | string | ✓ | ACK 集群 ID |
| `--config` | object | ✓ | 节点池请求体，JSON 对象或 @file |
| `--region` | string | ✓ | Alibaba Cloud region |

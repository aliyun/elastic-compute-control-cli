---
generated: true
generated_by: "website/scripts/gen-reference.mjs"
generated_command: "make build && npm --prefix website run gen:reference"
title: ack addon
sidebar_label: addon
description: "管理 ACK 集群组件"
---

# ack addon

管理 ACK 集群组件

运行 `ecctl ack addon <action> -h` 查看用法，或 `ecctl schema ack.addon.<action> --full` 获取该命令完整的结构化规格——每个参数与行为，便于 Agent 读取和填参。

## create

```bash
ecctl ack addon create <name> [flags]
```

在集群安装组件

- 类型：`mutation` · 风险：medium
- 同步：等待 `success`（waiter `task_succeeded`，超时 `3600s`）；用 `--no-wait` 跳过等待。

| API | 调用时机 | 用途 |
|---|---|---|
| `InstallClusterAddons` | 每次执行命令时 | 执行资源操作。 |
| `DescribeTaskInfo` | 未指定 `--no-wait` 时 | 轮询等待资源达到目标状态。（重复调用） |
| `DescribeClusterDetail` | 未指定 `--no-wait` 时 | 轮询等待资源达到目标状态。（重复调用） |
| `GetClusterAddonInstance` | 未指定 `--no-wait` 时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--cluster` | string | ✓ | ACK 集群 ID |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--version` | string | ✓ | 组件版本 |
| `--config` | string |  | 组件配置，支持 JSON/YAML 文本或 @file |

## update

```bash
ecctl ack addon update <name> [flags]
```

更新组件配置

- 类型：`mutation` · 风险：medium
- 同步：等待 `matched`（waiter `modify_task_visible`，超时 `600s`）；用 `--no-wait` 跳过等待。

| API | 调用时机 | 用途 |
|---|---|---|
| `DescribeClusterTasks` | 未指定 `--no-wait` 时 | 读取资源视图。 |
| `ModifyClusterAddon` | 每次执行命令时 | 执行资源操作。 |
| `DescribeClusterTasks` | 未指定 `--no-wait` 时 | 轮询等待资源达到目标状态。（重复调用） |
| `DescribeClusterTasks` | 未指定 `--no-wait` 时 | 返回最终资源视图。（复用等待结果，不额外请求） |
| `DescribeTaskInfo` | 未指定 `--no-wait` 时 | 轮询等待资源达到目标状态。（重复调用） |
| `DescribeClusterDetail` | 未指定 `--no-wait` 时 | 轮询等待资源达到目标状态。（重复调用） |
| `GetClusterAddonInstance` | 未指定 `--no-wait` 时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--cluster` | string | ✓ | ACK 集群 ID |
| `--config` | string | ✓ | 组件配置，支持 JSON/YAML 文本或 @file |
| `--region` | string | ✓ | Alibaba Cloud region |

## delete

```bash
ecctl ack addon delete [<names>...] [flags]
```

从集群卸载组件

- 类型：`mutation` · 风险：high
- 同步：等待 `success`（waiter `task_succeeded`，超时 `3600s`）；用 `--no-wait` 跳过等待。

| API | 调用时机 | 用途 |
|---|---|---|
| `UnInstallClusterAddons` | 每次执行命令时 | 执行资源操作。 |
| `DescribeTaskInfo` | 未指定 `--no-wait` 时 | 轮询等待资源达到目标状态。（重复调用） |
| `DescribeClusterDetail` | 未指定 `--no-wait` 时 | 轮询等待资源达到目标状态。（重复调用） |
| `ListClusterAddonInstances` | 未指定 `--no-wait` 时 | 轮询等待资源达到目标状态。（重复调用） |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--cluster` | string | ✓ | ACK 集群 ID |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--force` | boolean |  | 卸载时清理组件关联云资源（默认：`false`） |

## get

```bash
ecctl ack addon get <name> [flags]
```

查询组件实例或目录元信息

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `DescribeAddon` | 指定 `--catalog` 时 | 读取资源视图。 |
| `GetClusterAddonInstance` | 未指定 `--catalog` 时 | 读取资源视图。 |
| `ListClusterAddonInstanceResources` | 指定 `--with-resources` 且未指定 `--catalog` 时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--catalog` | boolean |  | 查询可安装组件目录 |
| `--cluster` | string |  | ACK 集群 ID |
| `--cluster-profile` | string |  | 目录查询的集群 profile 过滤条件 |
| `--cluster-spec` | string |  | 目录查询的集群规格过滤条件 |
| `--cluster-type` | string |  | 目录查询的集群类型过滤条件 |
| `--cluster-version` | string |  | 目录查询的 Kubernetes 版本过滤条件 |
| `--fields` | string |  | 要包含的资源字段，使用逗号分隔 |
| `--version` | string |  | 组件版本 |
| `--with-resources` | boolean |  | 附带组件实例关联的 Kubernetes 资源 |

## list

```bash
ecctl ack addon list [flags]
```

列出组件实例或目录元信息

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `ListAddons` | 指定 `--catalog` 时 | 读取资源视图。 |
| `ListClusterAddonInstances` | 未指定 `--catalog` 时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--catalog` | boolean |  | 查询可安装组件目录 |
| `--cluster` | string |  | ACK 集群 ID |
| `--cluster-profile` | string |  | 目录查询的集群 profile 过滤条件 |
| `--cluster-spec` | string |  | 目录查询的集群规格过滤条件 |
| `--cluster-type` | string |  | 目录查询的集群类型过滤条件 |
| `--cluster-version` | string |  | 目录查询的 Kubernetes 版本过滤条件 |
| `--fields` | string |  | 要包含的资源字段，使用逗号分隔 |

## upgrade

```bash
ecctl ack addon upgrade <name> [flags]
```

升级组件版本

- 类型：`mutation` · 风险：medium
- 同步：等待 `success`（waiter `task_succeeded`，超时 `3600s`）；用 `--no-wait` 跳过等待。

| API | 调用时机 | 用途 |
|---|---|---|
| `UpgradeClusterAddons` | 每次执行命令时 | 执行资源操作。 |
| `DescribeTaskInfo` | 未指定 `--no-wait` 时 | 轮询等待资源达到目标状态。（重复调用） |
| `DescribeClusterDetail` | 未指定 `--no-wait` 时 | 轮询等待资源达到目标状态。（重复调用） |
| `GetClusterAddonInstance` | 未指定 `--no-wait` 时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--cluster` | string | ✓ | ACK 集群 ID |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--version` | string | ✓ | 组件版本 |
| `--config` | string |  | 组件配置，支持 JSON/YAML 文本或 @file |
| `--policy` | string |  | 组件升级策略 |

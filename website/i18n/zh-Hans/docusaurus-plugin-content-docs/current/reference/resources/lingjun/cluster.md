---
title: lingjun cluster
sidebar_label: cluster
description: "管理灵骏集群资源"
---

# lingjun cluster

管理灵骏集群资源

运行 `ecctl lingjun cluster <action> -h` 查看用法，或 `ecctl schema lingjun.cluster.<action> --full` 获取该命令完整的结构化规格——每个参数与行为，便于 Agent 读取和填参。

## create

```bash
ecctl lingjun cluster create [flags]
```

创建灵骏集群

- 类型：`mutation` · 风险：medium
- 同步：等待 `running`（waiter `ready_after_change`，超时 `1800s`）；用 `--no-wait` 跳过等待。

| API | 调用时机 | 用途 |
|---|---|---|
| `CreateCluster` | 每次执行命令时 | 执行资源操作。 |
| `DescribeCluster` | 未指定 `--no-wait` 时 | 轮询等待资源达到目标状态。（重复调用） |
| `DescribeCluster` | 未指定 `--no-wait` 时 | 返回最终资源视图。（复用等待结果，不额外请求） |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--name` | string | ✓ | 灵骏集群名称 |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--cluster-type` | string |  | 灵骏集群类型 |
| `--components` | string |  | 符合灵骏 API 结构的组件 JSON 数组 |
| `--description` | string |  | 灵骏集群描述 |
| `--hpn-zone` | string |  | 集群 HPN 分区 |
| `--ignore-failed-node-tasks` | boolean |  | 在 API 支持时跳过失败节点任务 |
| `--networks` | string |  | 符合灵骏 API 结构的网络 JSON 对象 |
| `--nimiz-vswitches` | string |  | NIMIZ 交换机 JSON 数组 |
| `--node-groups` | string |  | 符合灵骏 API 结构的节点组 JSON 数组 |
| `--open-eni-jumbo-frame` | boolean |  | 启用 ENI Jumbo Frame |
| `--resource-group` | string |  | 资源组 ID |
| `--tag` | key_value |  | 标签赋值 key=value |

## update

```bash
ecctl lingjun cluster update <id> [flags]
```

扩容或缩容灵骏集群

- 类型：`mutation` · 风险：medium
- 同步：等待 `running`（waiter `ready_after_change`，超时 `1800s`）；用 `--no-wait` 跳过等待。

| API | 调用时机 | 用途 |
|---|---|---|
| `ExtendCluster` | 指定 `--extend` 时 | 执行资源操作。 |
| `DescribeCluster` | 指定 `--extend` 且未指定 `--no-wait` 时 | 轮询等待资源达到目标状态。（重复调用） |
| `ShrinkCluster` | 指定 `--shrink` 时 | 执行资源操作。 |
| `DescribeCluster` | 指定 `--shrink` 且未指定 `--no-wait` 时 | 轮询等待资源达到目标状态。（重复调用） |
| `DescribeCluster` | 未指定 `--no-wait` 时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--extend` | string |  | 集群扩容节点组 JSON 数组 |
| `--ignore-failed-node-tasks` | boolean |  | 在 API 支持时跳过失败节点任务 |
| `--ip-allocation-policy` | string |  | 扩容 IP 分配策略 JSON 数组 |
| `--shrink` | string |  | 集群缩容节点组 JSON 数组 |
| `--vpd-subnets` | string |  | 扩容 VPD 子网 ID JSON 数组 |
| `--vswitch-zone` | string |  | 扩容交换机可用区 ID |

## delete

```bash
ecctl lingjun cluster delete <id> [flags]
```

删除灵骏集群

- 类型：`mutation` · 风险：high
- 同步：等待 `absent`（waiter `deleted_after_delete`，超时 `1800s`）；用 `--no-wait` 跳过等待。

| API | 调用时机 | 用途 |
|---|---|---|
| `DeleteCluster` | 每次执行命令时 | 执行资源操作。 |
| `DescribeCluster` | 未指定 `--no-wait` 时 | 轮询等待资源达到目标状态。（重复调用） |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |

## get

```bash
ecctl lingjun cluster get <id> [flags]
```

查询灵骏集群

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `DescribeCluster` | 每次执行命令时 | 读取资源视图。 |
| `ListClusterNodes` | 指定 `--with-nodes` 时 | 读取资源视图。 |
| `ListClusterHyperNodes` | 指定 `--with-hyper-nodes` 时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | 要包含的资源字段，使用逗号分隔 |
| `--limit` | integer |  | 最多返回资源数量（默认：`100`） |
| `--next-token` | string |  | 下一页查询 Token |
| `--resource-group` | string |  | 资源组 ID |
| `--with-hyper-nodes` | boolean |  | 在 get 输出中包含集群超节点 |
| `--with-nodes` | boolean |  | 在 get 输出中包含集群节点 |

## list

```bash
ecctl lingjun cluster list [flags]
```

查询灵骏集群列表

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `ListClusters` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | 要包含的资源字段，使用逗号分隔 |
| `--filter` | key_value |  | 过滤表达式 key=value |
| `--limit` | integer |  | 最多返回资源数量（默认：`100`） |
| `--next-token` | string |  | 下一页查询 Token |

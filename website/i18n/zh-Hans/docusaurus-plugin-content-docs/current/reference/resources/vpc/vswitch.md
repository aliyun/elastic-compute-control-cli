---
generated: true
generated_by: "website/scripts/gen-reference.mjs"
generated_command: "make build && npm --prefix website run gen:reference"
title: vpc vswitch
sidebar_label: vswitch
description: "管理交换机资源"
---

# vpc vswitch

管理交换机资源

运行 `ecctl vpc vswitch <action> -h` 查看用法，或 `ecctl schema vpc.vswitch.<action> --full` 获取该命令完整的结构化规格——每个参数与行为，便于 Agent 读取和填参。

## create

```bash
ecctl vpc vswitch create [flags]
```

创建交换机

- 类型：`mutation` · 风险：medium
- 同步：等待 `Available`（waiter `available_after_create`，超时 `300s`）；用 `--no-wait` 跳过等待。
- 通过 `ClientToken` 幂等。

| API | 调用时机 | 用途 |
|---|---|---|
| `CreateVSwitch` | 每次执行命令时 | 执行资源操作。 |
| `DescribeVSwitchAttributes` | 未指定 `--no-wait` 时 | 轮询等待资源达到目标状态。（重复调用） |
| `DescribeVSwitchAttributes` | 未指定 `--no-wait` 时 | 返回最终资源视图。（复用等待结果，不额外请求） |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--cidr` | cidr | ✓ | CIDR 网段 |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--vpc` | string | ✓ | VPC ID |
| `--zone` | string | ✓ | 可用区 ID |
| `--description` | string |  | 交换机描述 |
| `--ipv6-cidr-block` | integer |  | IPv6 CIDR 网段序号 |
| `--name` | string |  | 交换机名称 |
| `--resource-group` | string |  | 资源组 ID |
| `--tag` | key_value |  | 标签赋值 key=value |
| `--vpc-ipv6-cidr` | string |  | VPC IPv6 CIDR 网段 |

## update

```bash
ecctl vpc vswitch update <id> [flags]
```

更新交换机属性

- 类型：`mutation` · 风险：medium
- 同步：等待 `Available`（waiter `available_after_update`，超时 `300s`）；用 `--no-wait` 跳过等待。

| API | 调用时机 | 用途 |
|---|---|---|
| `ModifyVSwitchAttribute` | 每次执行命令时 | 执行资源操作。 |
| `DescribeVSwitchAttributes` | 未指定 `--no-wait` 时 | 轮询等待资源达到目标状态。（重复调用） |
| `DescribeVSwitchAttributes` | 未指定 `--no-wait` 时 | 返回最终资源视图。（复用等待结果，不额外请求） |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--description` | string |  | 交换机描述 |
| `--enable-ipv6` | boolean |  | 启用 IPv6 |
| `--ipv6-cidr-block` | integer |  | IPv6 CIDR 网段序号 |
| `--name` | string |  | 交换机名称 |
| `--vpc-ipv6-cidr` | string |  | VPC IPv6 CIDR 网段 |

## delete

```bash
ecctl vpc vswitch delete <id> [flags]
```

删除交换机

- 类型：`mutation` · 风险：high
- 同步：等待 `absent`（waiter `deleted_after_delete`，超时 `300s`）；用 `--no-wait` 跳过等待。
- 支持 `--dry-run` 校验。

| API | 调用时机 | 用途 |
|---|---|---|
| `DeleteVSwitch` | 每次执行命令时 | 执行资源操作。 |
| `DescribeVSwitches` | 未指定 `--no-wait` 且未指定 `--dry-run` 时 | 轮询等待资源达到目标状态。（重复调用） |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |

## get

```bash
ecctl vpc vswitch get <id> [flags]
```

获取交换机

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `DescribeVSwitchAttributes` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | 要包含的资源字段，使用逗号分隔 |

## list

```bash
ecctl vpc vswitch list [<ids>...] [flags]
```

列出交换机资源

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `DescribeVSwitches` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | 要包含的资源字段，使用逗号分隔 |
| `--filter` | key_value |  | 过滤表达式 key=value |
| `--limit` | integer |  | 最多返回资源数量（默认：`50`） |
| `--page` | integer |  | 返回结果页码（默认：`1`） |

---
generated: true
generated_by: "website/scripts/gen-reference.mjs"
generated_command: "make build && npm --prefix website run gen:reference"
title: vpc
sidebar_label: vpc
description: "管理 VPC 资源"
---

# vpc

管理 VPC 资源

运行 `ecctl vpc <action> -h` 查看用法，或 `ecctl schema vpc.vpc.<action> --full` 获取该命令完整的结构化规格——每个参数与行为，便于 Agent 读取和填参。

## create

```bash
ecctl vpc create [flags]
```

创建 VPC

- 类型：`mutation` · 风险：medium
- 同步：等待 `Available`（waiter `available_after_create`，超时 `300s`）；用 `--no-wait` 跳过等待。
- 通过 `ClientToken` 幂等。
- 支持 `--dry-run` 校验。

| API | 调用时机 | 用途 |
|---|---|---|
| `CreateVpc` | 每次执行命令时 | 执行资源操作。 |
| `DescribeVpcAttribute` | 未指定 `--no-wait` 且未指定 `--dry-run` 时 | 轮询等待资源达到目标状态。（重复调用） |
| `DescribeVpcAttribute` | 未指定 `--no-wait` 且未指定 `--dry-run` 时 | 返回最终资源视图。（复用等待结果，不额外请求） |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--cidr` | cidr |  | CIDR 网段 |
| `--description` | string |  | VPC 描述 |
| `--enable-dns-hostname` | boolean |  | 启用 DNS 主机名 |
| `--enable-ipv6` | boolean |  | 启用 IPv6 |
| `--ipv4-cidr-mask` | integer |  | IPv4 IPAM CIDR 掩码 |
| `--ipv4-ipam-pool` | string |  | IPv4 IPAM 地址池 ID |
| `--ipv6-cidr` | string |  | IPv6 CIDR 网段 |
| `--ipv6-cidr-mask` | integer |  | IPv6 IPAM CIDR 掩码 |
| `--ipv6-ipam-pool` | string |  | IPv6 IPAM 地址池 ID |
| `--ipv6-isp` | string |  | IPv6 线路类型 |
| `--name` | string |  | VPC 名称 |
| `--resource-group` | string |  | 资源组 ID |
| `--tag` | key_value |  | 标签赋值 key=value |
| `--user-cidr` | string |  | 用户 CIDR 网段 |

## update

```bash
ecctl vpc update <id> [flags]
```

更新 VPC 属性

- 类型：`mutation` · 风险：medium
- 同步：等待 `Available`（waiter `available_after_update`，超时 `300s`）；用 `--no-wait` 跳过等待。

| API | 调用时机 | 用途 |
|---|---|---|
| `ModifyVpcAttribute` | 每次执行命令时 | 执行资源操作。 |
| `DescribeVpcAttribute` | 未指定 `--no-wait` 时 | 轮询等待资源达到目标状态。（重复调用） |
| `DescribeVpcAttribute` | 未指定 `--no-wait` 时 | 返回最终资源视图。（复用等待结果，不额外请求） |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--cidr` | cidr |  | CIDR 网段 |
| `--description` | string |  | VPC 描述 |
| `--enable-dns-hostname` | boolean |  | 启用 DNS 主机名 |
| `--enable-ipv6` | boolean |  | 启用 IPv6 |
| `--ipv6-cidr` | string |  | IPv6 CIDR 网段 |
| `--ipv6-isp` | string |  | IPv6 线路类型 |
| `--name` | string |  | VPC 名称 |

## delete

```bash
ecctl vpc delete <id> [flags]
```

删除 VPC

- 类型：`mutation` · 风险：high
- 同步：等待 `absent`（waiter `deleted_after_delete`，超时 `300s`）；用 `--no-wait` 跳过等待。
- 通过 `ClientToken` 幂等。
- 支持 `--dry-run` 校验。

| API | 调用时机 | 用途 |
|---|---|---|
| `DeleteVpc` | 每次执行命令时 | 执行资源操作。 |
| `DescribeVpcs` | 未指定 `--no-wait` 且未指定 `--dry-run` 时 | 轮询等待资源达到目标状态。（重复调用） |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--force` | boolean |  | 强制删除 VPC |

## get

```bash
ecctl vpc get <id> [flags]
```

获取 VPC

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `DescribeVpcAttribute` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | 要包含的资源字段，使用逗号分隔 |

## list

```bash
ecctl vpc list [<ids>...] [flags]
```

列出 VPC 资源

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `DescribeVpcs` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | 要包含的资源字段，使用逗号分隔 |
| `--filter` | key_value |  | 过滤表达式 key=value |
| `--limit` | integer |  | 最多返回资源数量（默认：`50`） |
| `--page` | integer |  | 返回结果页码（默认：`1`） |

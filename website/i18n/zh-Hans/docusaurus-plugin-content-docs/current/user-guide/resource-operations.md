---
title: 资源操作
description: 以同步、结构化的结果创建、查看、列举和删除资源。
---

# 资源操作

本页带一个资源走完整生命周期。下面的
JSON 已裁剪到相关字段。资源操作默认同步——模型见 [核心概念](./concepts.md)。

## 创建

创建命令在资源到达目标状态后才返回，并回读资源，因此响应是最终视图，
而不是进行中的操作。

```bash
ecctl vpc create --cidr 192.168.0.0/16 --name demo --region cn-beijing
```

```json
{
  "actions": [
    {"action_name": "CreateVpc", "request_id": "1A2B3C4D-5E6F-7A8B-9C0D-1E2F3A4B5C6D"},
    {"action_name": "DescribeVpcAttribute", "request_id": "2B3C4D5E-6F7A-8B9C-0D1E-2F3A4B5C6D7E"}
  ],
  "ecctl_capabilities_used": ["auto_wait"],
  "vpc": {
    "id": "vpc-2zexxxxxxxxxxxxxxxxx",
    "name": "demo",
    "cidr": "192.168.0.0/16",
    "status": "Available",
    "region": "cn-beijing",
    "creation_time": "2026-06-24T06:02:36Z"
  }
}
```

每个变更响应中都能看到三样东西：

- `actions` 列出每次阿里云 API 调用及其 `request_id`。这里创建（`CreateVpc`）
  之后紧跟回读（`DescribeVpcAttribute`）。
- `ecctl_capabilities_used` 报告 `auto_wait`，表示命令在返回前等待了目标状态。
- 资源对象（`vpc`）是最终视图，包含分配到的 `id` 和 `Available` 的 `status`。

在该 VPC 中创建 vSwitch 是同样的形态：

```bash
ecctl vpc vswitch create \
  --vpc vpc-2zexxxxxxxxxxxxxxxxx \
  --zone cn-beijing-h --cidr 192.168.1.0/24 \
  --name demo-vsw --region cn-beijing
```

```json
{
  "actions": [
    {"action_name": "CreateVSwitch"},
    {"action_name": "DescribeVSwitchAttributes"}
  ],
  "ecctl_capabilities_used": ["auto_wait"],
  "vswitch": {
    "id": "vsw-2zexxxxxxxxxxxxxxxxx",
    "vpc": "vpc-2zexxxxxxxxxxxxxxxxx",
    "zone": "cn-beijing-h",
    "cidr": "192.168.1.0/24",
    "available_ip_count": 252,
    "status": "Available"
  }
}
```

## 查看

`get` 按 ID 返回单个资源：

```bash
ecctl vpc get vpc-2zexxxxxxxxxxxxxxxxx --region cn-beijing
```

```json
{
  "vpc": {
    "id": "vpc-2zexxxxxxxxxxxxxxxxx",
    "name": "demo",
    "cidr": "192.168.0.0/16",
    "status": "Available",
    "cloud_resources": [
      {"resource_type": "VSwitch", "resource_count": 1},
      {"resource_type": "VRouter", "resource_count": 1},
      {"resource_type": "RouteTable", "resource_count": 1}
    ]
  }
}
```

## 列举与过滤

`list` 带分页，所有过滤条件都通过 `--filter key=value` 传入，
而不是为每个字段单独设计 flag：

```bash
ecctl vpc vswitch list --filter vpc=vpc-2zexxxxxxxxxxxxxxxxx --region cn-beijing
```

```json
{
  "pagination": {"page": 1, "limit": 50, "returned": 1, "has_more": false},
  "total": 1,
  "vswitches": [
    {
      "id": "vsw-2zexxxxxxxxxxxxxxxxx",
      "vpc": "vpc-2zexxxxxxxxxxxxxxxxx",
      "zone": "cn-beijing-h",
      "cidr": "192.168.1.0/24",
      "status": "Available"
    }
  ]
}
```

`pagination` 块给出当前页、页大小、返回条数，以及是否还有更多页。

## 删除

`delete` 同样是同步的：它等待资源消失，并报告 `deleted`。先删 vSwitch，再删 VPC。

```bash
ecctl vpc vswitch delete vsw-2zexxxxxxxxxxxxxxxxx --region cn-beijing
ecctl vpc delete vpc-2zexxxxxxxxxxxxxxxxx --region cn-beijing
```

```json
{
  "actions": [
    {"action_name": "DeleteVpc"},
    {"action_name": "DescribeVpcs"}
  ],
  "deleted": true,
  "ecctl_capabilities_used": ["auto_wait"],
  "vpc": {"id": "vpc-2zexxxxxxxxxxxxxxxxx"}
}
```

破坏性删除接受 `--force`。回读一个已删除的资源会返回结构化的 `not_found` 错误：

```bash
ecctl vpc get vpc-2zexxxxxxxxxxxxxxxxx --region cn-beijing
```

```json
{
  "error": {
    "kind": "not_found",
    "code": "NotFound",
    "message": "vpc not found",
    "retryable": false
  }
}
```

完整错误模型见 [输出、语言与错误](./output.md)。

## 控制等待

等待行为是契约的一部分。用 `schema` 读取：

```bash
ecctl schema vpc.vpc.create --brief
```

`vpc.vpc.create` 的 `contract.wait` 给出 waiter `available_after_create`、
默认 `timeout` 为 `300s`、退出等待的 flag `--no-wait`，以及 poll 命令
`ecctl vpc get <id> --region <region> --output json`。

按命令覆盖：

```bash
ecctl vpc create --cidr 192.168.0.0/16 --no-wait --region cn-beijing
ecctl vpc create --cidr 192.168.0.0/16 --timeout 600s --region cn-beijing
```

`--no-wait` 在到达目标状态前返回。`--timeout` 修改等待的上界。

## 校验与幂等

当契约报告支持 `dry_run` 时，可在不实际应用的情况下校验变更：

```bash
ecctl vpc create --cidr 192.168.0.0/16 --dry-run --region cn-beijing
```

支持幂等的变更携带 `ClientToken`。传入显式键，使重试的命令不会创建重复资源：

```bash
ecctl vpc create --cidr 192.168.0.0/16 --idempotency-key <token> --region cn-beijing
```

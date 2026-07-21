---
generated: true
generated_by: "website/scripts/gen-reference.mjs"
generated_command: "make build && npm --prefix website run gen:reference"
title: vpc
sidebar_label: vpc
description: "Manage VPC resources"
---

# vpc

Manage VPC resources

Run `ecctl vpc <action> -h` for usage, or `ecctl schema vpc.vpc.<action> --full` for the complete, agent-readable spec of every parameter and behavior.

## create

```bash
ecctl vpc create [flags]
```

Create a VPC

- Kind: `mutation` · Risk: medium
- Synchronous: waits for `Available` (waiter `available_after_create`, timeout `300s`); use `--no-wait` to skip.
- Idempotent via `ClientToken`.
- Dry run: supported via `--dry-run`.

| API | When called | Purpose |
|---|---|---|
| `CreateVpc` | Every time the command runs. | Perform the resource operation. |
| `DescribeVpcAttribute` | When `--no-wait` is not specified and `--dry-run` is not specified. | Poll until the resource reaches the target state. (repeated) |
| `DescribeVpcAttribute` | When `--no-wait` is not specified and `--dry-run` is not specified. | Return the final resource view. (cached; no additional request) |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--cidr` | cidr |  | CIDR block |
| `--description` | string |  | VPC description |
| `--enable-dns-hostname` | boolean |  | enable DNS hostnames |
| `--enable-ipv6` | boolean |  | enable IPv6 |
| `--ipv4-cidr-mask` | integer |  | IPv4 IPAM CIDR mask |
| `--ipv4-ipam-pool` | string |  | IPv4 IPAM pool ID |
| `--ipv6-cidr` | string |  | IPv6 CIDR block |
| `--ipv6-cidr-mask` | integer |  | IPv6 IPAM CIDR mask |
| `--ipv6-ipam-pool` | string |  | IPv6 IPAM pool ID |
| `--ipv6-isp` | string |  | IPv6 ISP |
| `--name` | string |  | VPC name |
| `--resource-group` | string |  | resource group ID |
| `--tag` | key_value |  | tag assignment key=value |
| `--user-cidr` | string |  | user CIDR block |

## update

```bash
ecctl vpc update <id> [flags]
```

Update VPC attributes

- Kind: `mutation` · Risk: medium
- Synchronous: waits for `Available` (waiter `available_after_update`, timeout `300s`); use `--no-wait` to skip.

| API | When called | Purpose |
|---|---|---|
| `ModifyVpcAttribute` | Every time the command runs. | Perform the resource operation. |
| `DescribeVpcAttribute` | When `--no-wait` is not specified. | Poll until the resource reaches the target state. (repeated) |
| `DescribeVpcAttribute` | When `--no-wait` is not specified. | Return the final resource view. (cached; no additional request) |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--cidr` | cidr |  | CIDR block |
| `--description` | string |  | VPC description |
| `--enable-dns-hostname` | boolean |  | enable DNS hostnames |
| `--enable-ipv6` | boolean |  | enable IPv6 |
| `--ipv6-cidr` | string |  | IPv6 CIDR block |
| `--ipv6-isp` | string |  | IPv6 ISP |
| `--name` | string |  | VPC name |

## delete

```bash
ecctl vpc delete <id> [flags]
```

Delete VPC

- Kind: `mutation` · Risk: high
- Synchronous: waits for `absent` (waiter `deleted_after_delete`, timeout `300s`); use `--no-wait` to skip.
- Idempotent via `ClientToken`.
- Dry run: supported via `--dry-run`.

| API | When called | Purpose |
|---|---|---|
| `DeleteVpc` | Every time the command runs. | Perform the resource operation. |
| `DescribeVpcs` | When `--no-wait` is not specified and `--dry-run` is not specified. | Poll until the resource reaches the target state. (repeated) |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--force` | boolean |  | force delete VPC |

## get

```bash
ecctl vpc get <id> [flags]
```

Get VPC

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `DescribeVpcAttribute` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | comma-separated resource fields to include |

## list

```bash
ecctl vpc list [<ids>...] [flags]
```

List VPC resources

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `DescribeVpcs` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | comma-separated resource fields to include |
| `--filter` | key_value |  | filter expression key=value |
| `--limit` | integer |  | maximum resources to return (default: `50`) |
| `--page` | integer |  | results page to return (default: `1`) |

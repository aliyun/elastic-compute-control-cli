---
title: vpc vswitch
sidebar_label: vswitch
description: "Manage VSwitch resources"
---

# vpc vswitch

Manage VSwitch resources

Run `ecctl vpc vswitch <action> -h` for usage, or `ecctl schema vpc.vswitch.<action> --full` for the complete, agent-readable spec of every parameter and behavior.

## create

```bash
ecctl vpc vswitch create [flags]
```

Create VSwitch

- Kind: `mutation` · Risk: medium
- Synchronous: waits for `Available` (waiter `available_after_create`, timeout `300s`); use `--no-wait` to skip.
- Idempotent via `ClientToken`.

| API | When called | Purpose |
|---|---|---|
| `CreateVSwitch` | Every time the command runs. | Perform the resource operation. |
| `DescribeVSwitchAttributes` | When `--no-wait` is not specified. | Poll until the resource reaches the target state. (repeated) |
| `DescribeVSwitchAttributes` | When `--no-wait` is not specified. | Return the final resource view. (cached; no additional request) |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--cidr` | cidr | ✓ | CIDR block |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--vpc` | string | ✓ | VPC ID |
| `--zone` | string | ✓ | zone ID |
| `--description` | string |  | VSwitch description |
| `--ipv6-cidr-block` | integer |  | IPv6 CIDR block index |
| `--name` | string |  | VSwitch name |
| `--resource-group` | string |  | resource group ID |
| `--tag` | key_value |  | tag assignment key=value |
| `--vpc-ipv6-cidr` | string |  | VPC IPv6 CIDR block |

## update

```bash
ecctl vpc vswitch update <id> [flags]
```

Update VSwitch attributes

- Kind: `mutation` · Risk: medium
- Synchronous: waits for `Available` (waiter `available_after_update`, timeout `300s`); use `--no-wait` to skip.

| API | When called | Purpose |
|---|---|---|
| `ModifyVSwitchAttribute` | Every time the command runs. | Perform the resource operation. |
| `DescribeVSwitchAttributes` | When `--no-wait` is not specified. | Poll until the resource reaches the target state. (repeated) |
| `DescribeVSwitchAttributes` | When `--no-wait` is not specified. | Return the final resource view. (cached; no additional request) |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--description` | string |  | VSwitch description |
| `--enable-ipv6` | boolean |  | enable IPv6 |
| `--ipv6-cidr-block` | integer |  | IPv6 CIDR block index |
| `--name` | string |  | VSwitch name |
| `--vpc-ipv6-cidr` | string |  | VPC IPv6 CIDR block |

## delete

```bash
ecctl vpc vswitch delete <id> [flags]
```

Delete VSwitch

- Kind: `mutation` · Risk: high
- Synchronous: waits for `absent` (waiter `deleted_after_delete`, timeout `300s`); use `--no-wait` to skip.
- Dry run: supported via `--dry-run`.

| API | When called | Purpose |
|---|---|---|
| `DeleteVSwitch` | Every time the command runs. | Perform the resource operation. |
| `DescribeVSwitches` | When `--no-wait` is not specified and `--dry-run` is not specified. | Poll until the resource reaches the target state. (repeated) |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |

## get

```bash
ecctl vpc vswitch get <id> [flags]
```

Get VSwitch

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `DescribeVSwitchAttributes` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | comma-separated resource fields to include |

## list

```bash
ecctl vpc vswitch list [<ids>...] [flags]
```

List VSwitch resources

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `DescribeVSwitches` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | comma-separated resource fields to include |
| `--filter` | key_value |  | filter expression key=value |
| `--limit` | integer |  | maximum resources to return (default: `50`) |
| `--page` | integer |  | results page to return (default: `1`) |

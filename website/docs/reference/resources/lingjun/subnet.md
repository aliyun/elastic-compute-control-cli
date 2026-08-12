---
generated: true
generated_by: "website/scripts/gen-reference.mjs"
generated_command: "make build && npm --prefix website run gen:reference"
title: lingjun subnet
sidebar_label: subnet
description: "Manage Lingjun subnet resources"
---

# lingjun subnet

Manage Lingjun subnet resources

Run `ecctl lingjun subnet <action> -h` for usage, or `ecctl schema lingjun.subnet.<action> --full` for the complete, agent-readable spec of every parameter and behavior.

## create

```bash
ecctl lingjun subnet create [flags]
```

Create Lingjun subnet

- Kind: `mutation` · Risk: medium
- Synchronous: waits for `Available` (waiter `available_after_change`, timeout `300s`); use `--no-wait` to skip.

| API | When called | Purpose |
|---|---|---|
| `CreateSubnet` | Every time the command runs. | Perform the resource operation. |
| `GetSubnet` | When `--no-wait` is not specified. | Poll until the resource reaches the target state. (repeated) |
| `GetSubnet` | When `--no-wait` is not specified. | Return the final resource view. (cached; no additional request) |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--cidr` | cidr | ✓ | subnet CIDR block |
| `--name` | string | ✓ | Lingjun subnet name |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--vpd` | string | ✓ | Lingjun VPD ID |
| `--zone` | string | ✓ | zone ID |
| `--tag` | key_value |  | tag assignment key=value |
| `--type` | string |  | subnet usage type |

## update

```bash
ecctl lingjun subnet update <id> [flags]
```

Update Lingjun subnet

- Kind: `mutation` · Risk: medium
- Synchronous: waits for `Available` (waiter `available_after_change`, timeout `300s`); use `--no-wait` to skip.

| API | When called | Purpose |
|---|---|---|
| `UpdateSubnet` | Every time the command runs. | Perform the resource operation. |
| `GetSubnet` | When `--no-wait` is not specified. | Poll until the resource reaches the target state. (repeated) |
| `GetSubnet` | When `--no-wait` is not specified. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--vpd` | string | ✓ | Lingjun VPD ID |
| `--zone` | string | ✓ | zone ID |
| `--name` | string |  | Lingjun subnet name |

## delete

```bash
ecctl lingjun subnet delete <id> [flags]
```

Delete Lingjun subnet

- Kind: `mutation` · Risk: high
- Synchronous: waits for `absent` (waiter `deleted_after_delete`, timeout `300s`); use `--no-wait` to skip.

| API | When called | Purpose |
|---|---|---|
| `DeleteSubnet` | Every time the command runs. | Perform the resource operation. |
| `ListSubnets` | When `--no-wait` is not specified. | Poll until the resource reaches the target state. (repeated) |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--vpd` | string | ✓ | Lingjun VPD ID |
| `--zone` | string | ✓ | zone ID |

## get

```bash
ecctl lingjun subnet get <id> [flags]
```

Get Lingjun subnet

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `GetSubnet` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | comma-separated resource fields to include |
| `--vpd` | string |  | Lingjun VPD ID |

## list

```bash
ecctl lingjun subnet list [flags]
```

List Lingjun subnets

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `ListSubnets` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | comma-separated resource fields to include |
| `--filter` | key_value |  | filter expression key=value |
| `--limit` | integer |  | maximum resources to return (default: `100`) |
| `--page` | integer |  | results page to return (default: `1`) |

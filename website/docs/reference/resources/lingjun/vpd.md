---
generated: true
generated_by: "website/scripts/gen-reference.mjs"
generated_command: "make build && npm --prefix website run gen:reference"
title: lingjun vpd
sidebar_label: vpd
description: "Manage Lingjun VPD resources"
---

# lingjun vpd

Manage Lingjun VPD resources

Run `ecctl lingjun vpd <action> -h` for usage, or `ecctl schema lingjun.vpd.<action> --full` for the complete, agent-readable spec of every parameter and behavior.

## create

```bash
ecctl lingjun vpd create [flags]
```

Create VPD

- Kind: `mutation` · Risk: medium
- Synchronous: waits for `Available` (waiter `available_after_change`, timeout `300s`); use `--no-wait` to skip.

| API | When called | Purpose |
|---|---|---|
| `CreateVpd` | Every time the command runs. | Perform the resource operation. |
| `GetVpd` | When `--no-wait` is not specified. | Poll until the resource reaches the target state. (repeated) |
| `GetVpd` | When `--no-wait` is not specified. | Return the final resource view. (cached; no additional request) |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--cidr` | cidr | ✓ | VPD CIDR block |
| `--name` | string | ✓ | VPD name |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--resource-group` | string |  | resource group ID |
| `--subnet` | object |  | subnets to create with the VPD |
| `--tag` | key_value |  | tag assignment key=value |

## update

```bash
ecctl lingjun vpd update <id> [flags]
```

Update VPD

- Kind: `mutation` · Risk: medium
- Synchronous: waits for `matched` (waiter `update_converged`, timeout `300s`); use `--no-wait` to skip.

| API | When called | Purpose |
|---|---|---|
| `UpdateVpd` | When `--name` is specified. | Perform the resource operation. |
| `AssociateVpdCidrBlock` | When `--cidr` contains a value prefixed with `+`. | Perform the resource operation. |
| `UnAssociateVpdCidrBlock` | When `--cidr` contains a value prefixed with `-`. | Perform the resource operation. |
| `GetVpd` | When `--no-wait` is not specified. | Poll until the resource reaches the target state. (repeated) |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--cidr` | string |  | secondary CIDR change; prefix with + to associate or - to unassociate |
| `--name` | string |  | VPD name |

## delete

```bash
ecctl lingjun vpd delete <id> [flags]
```

Delete VPD

- Kind: `mutation` · Risk: high
- Synchronous: waits for `absent` (waiter `deleted_after_delete`, timeout `300s`); use `--no-wait` to skip.

| API | When called | Purpose |
|---|---|---|
| `DeleteVpd` | Every time the command runs. | Perform the resource operation. |
| `ListVpds` | When `--no-wait` is not specified. | Poll until the resource reaches the target state. (repeated) |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |

## get

```bash
ecctl lingjun vpd get <id> [flags]
```

Get VPD

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `GetVpd` | Every time the command runs. | Read the resource view. |
| `ListVpdRouteEntries` | When `--with-routes` is specified. | Read the resource view. |
| `ListVpdGrantRules` | When `--with-grants` is specified. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | comma-separated resource fields to include |
| `--limit` | integer |  | maximum resources to return (default: `100`) |
| `--page` | integer |  | results page to return (default: `1`) |
| `--with-grants` | boolean |  | include VPD grant rules |
| `--with-routes` | boolean |  | include VPD route entries |

## list

```bash
ecctl lingjun vpd list [flags]
```

List VPDs

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `ListVpds` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | comma-separated resource fields to include |
| `--filter` | key_value |  | filter expression key=value |
| `--limit` | integer |  | maximum resources to return (default: `100`) |
| `--page` | integer |  | results page to return (default: `1`) |

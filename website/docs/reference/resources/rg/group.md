---
generated: true
generated_by: "website/scripts/gen-reference.mjs"
generated_command: "make build && npm --prefix website run gen:reference"
title: rg group
sidebar_label: group
description: "Manage resource groups"
---

# rg group

Manage resource groups

Run `ecctl rg group <action> -h` for usage, or `ecctl schema rg.group.<action> --full` for the complete, agent-readable spec of every parameter and behavior.

## create

```bash
ecctl rg group create [flags]
```

Create resource group

- Kind: `mutation` · Risk: medium
- Synchronous: waits for `OK` (waiter `ok_after_create`, timeout `10s`); use `--no-wait` to skip.

| API | When called | Purpose |
|---|---|---|
| `CreateResourceGroup` | Every time the command runs. | Perform the resource operation. |
| `GetResourceGroup` | Every time the command runs. | Poll until the resource reaches the target state. (repeated) |
| `GetResourceGroup` | Every time the command runs. | Return the final resource view. (cached; no additional request) |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--display-name` | string | ✓ | resource group display name |
| `--name` | string | ✓ | resource group identifier |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--tag` | key_value |  | tag assignment key=value |

## update

```bash
ecctl rg group update <id> [flags]
```

Update resource group

- Kind: `mutation` · Risk: medium

| API | When called | Purpose |
|---|---|---|
| `UpdateResourceGroup` | Every time the command runs. | Perform the resource operation. |
| `GetResourceGroup` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--display-name` | string |  | resource group display name |

## delete

```bash
ecctl rg group delete <id> [flags]
```

Delete resource group

- Kind: `mutation` · Risk: high

| API | When called | Purpose |
|---|---|---|
| `DeleteResourceGroup` | Every time the command runs. | Perform the resource operation. |
| `ListResourceGroups` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |

## get

```bash
ecctl rg group get <id> [flags]
```

Get resource group

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `GetResourceGroup` | Every time the command runs. | Read the resource view. |
| `GetResourceGroupResourceCounts` | When `--with-counts` is specified. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | comma-separated resource fields to include |
| `--with-counts` | boolean |  | include resource counts |

## list

```bash
ecctl rg group list [<ids>...] [flags]
```

List resource groups

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `ListResourceGroups` | When `--with-auth-details` is not specified. | Read the resource view. |
| `ListResourceGroupsWithAuthDetails` | When `--with-auth-details` is specified. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | comma-separated resource fields to include |
| `--filter` | key_value |  | filter expression key=value |
| `--limit` | integer |  | maximum resources to return (default: `100`) |
| `--page` | integer |  | results page to return (default: `1`) |
| `--with-auth-details` | boolean |  | include authorization details |

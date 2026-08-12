---
generated: true
generated_by: "website/scripts/gen-reference.mjs"
generated_command: "make build && npm --prefix website run gen:reference"
title: lingjun node-group
sidebar_label: node-group
description: "Manage Lingjun node group resources"
---

# lingjun node-group

Manage Lingjun node group resources

Run `ecctl lingjun node-group <action> -h` for usage, or `ecctl schema lingjun.node-group.<action> --full` for the complete, agent-readable spec of every parameter and behavior.

## create

```bash
ecctl lingjun node-group create [flags]
```

Create Lingjun node group

- Kind: `mutation` · Risk: medium

| API | When called | Purpose |
|---|---|---|
| `CreateNodeGroup` | Every time the command runs. | Perform the resource operation. |
| `DescribeNodeGroup` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--cluster` | string | ✓ | parent cluster ID |
| `--name` | string | ✓ | node group name |
| `--node-group` | string | ✓ | node group configuration JSON object matching the CreateNodeGroup NodeGroup body shape (e.g. &#123;"NodeGroupName":"...","Az":"...","MachineType":"...","ImageId":"...",...}); pass inline JSON or @file path |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--node-unit` | string |  | node unit configuration JSON object matching the CreateNodeGroup NodeUnit body shape (e.g. &#123;"NodeUnitId":"...","NodeUnitName":"...","ResourceGroupId":"...","MaxNodes":...}); pass inline JSON or @file path |

## update

```bash
ecctl lingjun node-group update <id> [flags]
```

Update Lingjun node group

- Kind: `mutation` · Risk: medium

| API | When called | Purpose |
|---|---|---|
| `UpdateNodeGroup` | Every time the command runs. | Perform the resource operation. |
| `DescribeNodeGroup` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--file-system-mount-enabled` | boolean |  | enable file system mount |
| `--image` | string |  | image ID |
| `--key-pair` | string |  | key pair name |
| `--login-password` | string |  | login password (sensitive: prefer --key-pair; inject via @file or environment variable to avoid leaking through shell history) |
| `--name` | string |  | node group name |
| `--ram-role` | string |  | RAM role name |
| `--user-data` | string |  | user data script |

## delete

```bash
ecctl lingjun node-group delete <id> [flags]
```

Delete Lingjun node group

- Kind: `mutation` · Risk: high
- Synchronous: waits for `absent` (waiter `deleted_after_delete`, timeout `600s`); use `--no-wait` to skip.

| API | When called | Purpose |
|---|---|---|
| `DeleteNodeGroup` | Every time the command runs. | Perform the resource operation. |
| `DescribeNodeGroup` | When `--no-wait` is not specified. | Poll until the resource reaches the target state. (repeated) |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--cluster` | string |  | parent cluster ID |

## get

```bash
ecctl lingjun node-group get <id> [flags]
```

Get Lingjun node group details

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `DescribeNodeGroup` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | comma-separated resource fields to include |

## list

```bash
ecctl lingjun node-group list [flags]
```

List Lingjun node groups

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `ListNodeGroups` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--cluster` | string |  | parent cluster ID |
| `--fields` | string |  | comma-separated resource fields to include |
| `--filter` | key_value |  | filter expression key=value |
| `--limit` | integer |  | maximum resources to return (default: `100`) |
| `--next-token` | string |  | token for the next result page |

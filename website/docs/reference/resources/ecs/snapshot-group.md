---
generated: true
generated_by: "website/scripts/gen-reference.mjs"
generated_command: "make build && npm --prefix website run gen:reference"
title: ecs snapshot-group
sidebar_label: snapshot-group
description: "Manage snapshot-consistent groups"
---

# ecs snapshot-group

Manage snapshot-consistent groups

Run `ecctl ecs snapshot-group <action> -h` for usage, or `ecctl schema ecs.snapshot-group.<action> --full` for the complete, agent-readable spec of every parameter and behavior.

## create

```bash
ecctl ecs snapshot-group create [flags]
```

Create snapshot group

- Kind: `mutation` · Risk: medium
- Synchronous: waits for `accomplished` (waiter `accomplished_after_create`, timeout `600s`); use `--no-wait` to skip.
- Idempotent via `ClientToken`.

| API | When called | Purpose |
|---|---|---|
| `CreateSnapshotGroup` | Every time the command runs. | Perform the resource operation. |
| `DescribeSnapshotGroups` | When `--no-wait` is not specified. | Poll until the resource reaches the target state. (repeated) |
| `DescribeSnapshotGroups` | When `--no-wait` is not specified. | Return the final resource view. (cached; no additional request) |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--instance` | string | ✓ | source instance ID |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--description` | string |  | snapshot group description |
| `--disk-ids` | array |  | disk IDs to include in the group |
| `--exclude-disk-ids` | array |  | disk IDs to exclude from the group |
| `--instant-access` | boolean |  | enable instant access for the group snapshots |
| `--instant-access-retention-days` | integer |  | instant access retention days |
| `--name` | string |  | snapshot group name |
| `--resource-group` | string |  | resource group ID |
| `--tag` | key_value |  | tag assignment key=value |

## update

```bash
ecctl ecs snapshot-group update <id> [flags]
```

Update snapshot group

- Kind: `mutation` · Risk: medium

| API | When called | Purpose |
|---|---|---|
| `ModifySnapshotGroup` | Every time the command runs. | Perform the resource operation. |
| `DescribeSnapshotGroups` | When `--no-wait` is not specified. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--description` | string |  | snapshot group description |
| `--name` | string |  | snapshot group name |

## delete

```bash
ecctl ecs snapshot-group delete <id> [flags]
```

Delete snapshot group

- Kind: `mutation` · Risk: high
- Synchronous: waits for `absent` (waiter `deleted_after_delete`, timeout `300s`); use `--no-wait` to skip.

| API | When called | Purpose |
|---|---|---|
| `DeleteSnapshotGroup` | Every time the command runs. | Perform the resource operation. |
| `DescribeSnapshotGroups` | When `--no-wait` is not specified. | Poll until the resource reaches the target state. (repeated) |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |

## get

```bash
ecctl ecs snapshot-group get <id> [flags]
```

Get snapshot group

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `DescribeSnapshotGroups` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | comma-separated resource fields to include |

## list

```bash
ecctl ecs snapshot-group list [<ids>...] [flags]
```

List snapshot groups

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `DescribeSnapshotGroups` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | comma-separated resource fields to include |
| `--filter` | key_value |  | filter expression key=value |
| `--limit` | integer |  | maximum resources to return (default: `100`) |
| `--next-token` | string |  | token for the next result page |

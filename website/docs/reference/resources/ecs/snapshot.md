---
title: ecs snapshot
sidebar_label: snapshot
description: "Manage disk snapshots"
---

# ecs snapshot

Manage disk snapshots

Run `ecctl ecs snapshot <action> -h` for usage, or `ecctl schema ecs.snapshot.<action> --full` for the complete, agent-readable spec of every parameter and behavior.

## create

```bash
ecctl ecs snapshot create [flags]
```

Create snapshot

- Kind: `mutation` · Risk: medium
- Synchronous: waits for `accomplished` (waiter `accomplished_after_create`, timeout `600s`); use `--no-wait` to skip.
- Idempotent via `ClientToken`.

| API | When called | Purpose |
|---|---|---|
| `CreateSnapshot` | Every time the command runs. | Perform the resource operation. |
| `DescribeSnapshots` | When `--no-wait` is not specified. | Poll until the resource reaches the target state. (repeated) |
| `DescribeSnapshots` | When `--no-wait` is not specified. | Return the final resource view. (cached; no additional request) |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--disk` | string | ✓ | source disk ID |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--category` | string |  | snapshot category |
| `--description` | string |  | snapshot description |
| `--instant-access` | boolean |  | enable instant access |
| `--instant-access-retention-days` | integer |  | instant access retention days |
| `--name` | string |  | snapshot name |
| `--resource-group` | string |  | resource group ID |
| `--retention-days` | integer |  | snapshot retention days |
| `--tag` | key_value |  | tag assignment key=value |

## update

```bash
ecctl ecs snapshot update <id> [flags]
```

Update snapshot attributes, category, or lock state

- Kind: `mutation` · Risk: medium

| API | When called | Purpose |
|---|---|---|
| `ModifySnapshotAttribute` | When `--name` is specified or `--description` is specified or `--disable-instant-access` is specified. | Perform the resource operation. |
| `ModifySnapshotCategory` | When `--category` is specified. | Perform the resource operation. |
| `LockSnapshot` | When `--lock` is specified. | Perform the resource operation. |
| `UnlockSnapshot` | When `--unlock` is specified. | Perform the resource operation. |
| `OpenSnapshotService` | When `--open-service` is specified. | Perform the resource operation. |
| `DescribeSnapshots` | When `--no-wait` is not specified. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--category` | string |  | snapshot category |
| `--description` | string |  | snapshot description |
| `--disable-instant-access` | boolean |  | disable instant access on update |
| `--lock` | boolean |  | lock the snapshot against deletion |
| `--name` | string |  | snapshot name |
| `--open-service` | boolean |  | activate the snapshot service |
| `--unlock` | boolean |  | unlock the snapshot |

## delete

```bash
ecctl ecs snapshot delete <id> [flags]
```

Delete snapshot

- Kind: `mutation` · Risk: high
- Synchronous: waits for `absent` (waiter `deleted_after_delete`, timeout `300s`); use `--no-wait` to skip.

| API | When called | Purpose |
|---|---|---|
| `DeleteSnapshot` | Every time the command runs. | Perform the resource operation. |
| `DescribeSnapshots` | When `--no-wait` is not specified. | Poll until the resource reaches the target state. (repeated) |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--force` | boolean |  | force delete the snapshot even if it is used by a disk (must be set explicitly) (default: `false`) |

## get

```bash
ecctl ecs snapshot get <id> [flags]
```

Get snapshot

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `DescribeSnapshots` | Every time the command runs. | Read the resource view. |
| `DescribeLockedSnapshots` | When `--with-lock` is specified. | Read the resource view. |
| `DescribeSnapshotLinks` | When `--with-links` is specified. | Read the resource view. |
| `DescribeSnapshotMonitorData` | When `--with-monitor` is specified. | Read the resource view. |
| `DescribeSnapshotPackage` | When `--with-package` is specified. | Read the resource view. |
| `DescribeSnapshotsUsage` | When `--with-usage` is specified. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--disk` | string |  | source disk ID |
| `--end-time` | string |  | monitor query end time |
| `--fields` | string |  | comma-separated resource fields to include |
| `--instance` | string |  | source instance ID |
| `--period` | integer |  | monitor data period in seconds |
| `--start-time` | string |  | monitor query start time |
| `--with-links` | boolean |  | include snapshot chain information |
| `--with-lock` | boolean |  | include snapshot lock information |
| `--with-monitor` | boolean |  | include snapshot capacity monitor data |
| `--with-package` | boolean |  | include OSS snapshot storage package information |
| `--with-usage` | boolean |  | include snapshot count and capacity usage |

## list

```bash
ecctl ecs snapshot list [<ids>...] [flags]
```

List snapshots

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `DescribeSnapshots` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | comma-separated resource fields to include |
| `--filter` | key_value |  | filter expression key=value |
| `--limit` | integer |  | maximum resources to return (default: `100`) |
| `--next-token` | string |  | token for the next result page |

## copy

```bash
ecctl ecs snapshot copy <id> [flags]
```

Copy snapshot across regions

- Kind: `mutation` · Risk: medium
- Synchronous: waits for `accomplished` (waiter `accomplished_after_copy`, timeout `600s`); use `--no-wait` to skip.
- Idempotent via `ClientToken`.

| API | When called | Purpose |
|---|---|---|
| `CopySnapshot` | Every time the command runs. | Perform the resource operation. |
| `DescribeSnapshots` | When `--no-wait` is not specified. | Poll until the resource reaches the target state. (repeated) |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--destination-region` | string | ✓ | destination region for copy |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--destination-description` | string |  | destination snapshot description for copy |
| `--destination-name` | string |  | destination snapshot name for copy |
| `--encrypted` | boolean |  | encrypt the destination snapshot when copying |
| `--kms-key-id` | string |  | KMS key ID used to encrypt the destination snapshot |
| `--resource-group` | string |  | resource group ID |
| `--retention-days` | integer |  | snapshot retention days |
| `--tag` | key_value |  | tag assignment key=value |

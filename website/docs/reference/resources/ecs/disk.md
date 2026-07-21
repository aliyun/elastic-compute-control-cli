---
generated: true
generated_by: "website/scripts/gen-reference.mjs"
generated_command: "make build && npm --prefix website run gen:reference"
title: ecs disk
sidebar_label: disk
description: "Manage disk resources"
---

# ecs disk

Manage disk resources

Run `ecctl ecs disk <action> -h` for usage, or `ecctl schema ecs.disk.<action> --full` for the complete, agent-readable spec of every parameter and behavior.

## create

```bash
ecctl ecs disk create [flags]
```

Create disk

- Kind: `mutation` · Risk: medium
- Synchronous: waits for `Available` (waiter `available_after_create`, timeout `300s`); use `--no-wait` to skip.
- Idempotent via `ClientToken`.

| API | When called | Purpose |
|---|---|---|
| `CreateDisk` | Every time the command runs. | Perform the resource operation. |
| `DescribeDisks` | When `--no-wait` is not specified. | Poll until the resource reaches the target state. (repeated) |
| `DescribeDisks` | When `--no-wait` is not specified. | Return the final resource view. (cached; no additional request) |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--bursting-enabled` | boolean |  | enable performance burst |
| `--category` | string |  | disk category |
| `--description` | string |  | disk description |
| `--encrypted` | boolean |  | encrypt the disk |
| `--kms-key-id` | string |  | KMS key ID for disk encryption |
| `--name` | string |  | disk name |
| `--performance-level` | string |  | ESSD performance level |
| `--provisioned-iops` | integer |  | provisioned IOPS |
| `--resource-group` | string |  | resource group ID |
| `--size` | integer |  | disk size in GiB |
| `--snapshot` | string |  | snapshot ID |
| `--tag` | key_value |  | tag assignment key=value |
| `--zone` | string |  | zone ID |

## update

```bash
ecctl ecs disk update <id> [flags]
```

Update disk

- Kind: `mutation` · Risk: medium
- Idempotent via `ClientToken`.

| API | When called | Purpose |
|---|---|---|
| `ModifyDiskAttribute` | When `--name` is specified or `--description` is specified or `--delete-with-instance` is specified or `--delete-auto-snapshot` is specified. | Perform the resource operation. |
| `ResizeDisk` | When `--size` is specified. | Perform the resource operation. |
| `ModifyDiskSpec` | When `--category` is specified or `--performance-level` is specified or `--provisioned-iops` is specified or `--bursting-enabled` is specified. | Perform the resource operation. |
| `ModifyDiskChargeType` | When `--charge-type` is specified. | Perform the resource operation. |
| `ModifyDiskDeployment` | When `--storage-cluster` is specified. | Perform the resource operation. |
| `EnableDiskEncryptionByDefault` | When `--encryption-default` equals `enable`. | Perform the resource operation. |
| `DisableDiskEncryptionByDefault` | When `--encryption-default` equals `disable`. | Perform the resource operation. |
| `ModifyDiskDefaultKMSKeyId` | When `--default-kms-key-id` is specified. | Perform the resource operation. |
| `ResetDiskDefaultKMSKeyId` | When `--reset-default-kms-key` is specified. | Perform the resource operation. |
| `DescribeDisks` | When `&lt;id>` is specified and `--no-wait` is not specified. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--auto-pay` | boolean |  | automatically pay for prepaid disks |
| `--bursting-enabled` | boolean |  | enable performance burst |
| `--category` | string |  | disk category |
| `--charge-type` | string |  | disk charge type |
| `--default-kms-key-id` | string |  | account-level default disk encryption KMS key ID |
| `--delete-auto-snapshot` | boolean |  | delete automatic snapshots with the disk |
| `--delete-with-instance` | boolean |  | release the disk with the instance |
| `--description` | string |  | disk description |
| `--encryption-default` | string |  | enable or disable account-level default disk encryption |
| `--instance` | string |  | instance ID |
| `--name` | string |  | disk name |
| `--performance-level` | string |  | ESSD performance level |
| `--provisioned-iops` | integer |  | provisioned IOPS |
| `--reset-default-kms-key` | boolean |  | reset account-level default disk encryption KMS key |
| `--resize-type` | string |  | resize mode |
| `--size` | integer |  | disk size in GiB |
| `--storage-cluster` | string |  | storage cluster ID for disk deployment migration |

## delete

```bash
ecctl ecs disk delete <id> [flags]
```

Delete disk

- Kind: `mutation` · Risk: high
- Synchronous: waits for `absent` (waiter `deleted_after_delete`, timeout `300s`); use `--no-wait` to skip.

| API | When called | Purpose |
|---|---|---|
| `DeleteDisk` | Every time the command runs. | Perform the resource operation. |
| `DescribeDisks` | When `--no-wait` is not specified. | Poll until the resource reaches the target state. (repeated) |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--force` | boolean |  | force the disk operation when supported by the API (default: `false`) |

## get

```bash
ecctl ecs disk get <id> [flags]
```

Get disk

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `DescribeDiskEncryptionByDefaultStatus` | When `--encryption-default` is specified. | Read the resource view. |
| `DescribeDiskDefaultKMSKeyId` | When `--default-kms-key` is specified. | Read the resource view. |
| `DescribeDisks` | When `&lt;id>` is specified. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--default-kms-key` | boolean |  | query account-level default disk encryption KMS key |
| `--encryption-default` | boolean |  | query account-level default disk encryption status |
| `--fields` | string |  | comma-separated resource fields to include |

## list

```bash
ecctl ecs disk list [<ids>...] [flags]
```

List disks

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `DescribeDisks` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | comma-separated resource fields to include |
| `--filter` | key_value |  | filter expression key=value |
| `--limit` | integer |  | maximum resources to return (default: `100`) |
| `--next-token` | string |  | token for the next result page |

## attach

```bash
ecctl ecs disk attach <id> [flags]
```

Attach disk

- Kind: `mutation` · Risk: medium
- Synchronous: waits for `In_use` (waiter `in_use_after_attach`, timeout `300s`); use `--no-wait` to skip.

| API | When called | Purpose |
|---|---|---|
| `AttachDisk` | Every time the command runs. | Perform the resource operation. |
| `DescribeDisks` | When `--no-wait` is not specified. | Poll until the resource reaches the target state. (repeated) |
| `DescribeDisks` | When `--no-wait` is not specified. | Return the final resource view. (cached; no additional request) |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--instance` | string | ✓ | instance ID |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--delete-with-instance` | boolean |  | release the disk with the instance |
| `--device` | string |  | device name in the instance |
| `--force` | boolean |  | force the disk operation when supported by the API (default: `false`) |

## clone

```bash
ecctl ecs disk clone <id> [flags]
```

Clone disk

- Kind: `mutation` · Risk: medium
- Synchronous: waits for `Finished` (waiter `clone_finished`, timeout `3600s`); use `--no-wait` to skip.
- Idempotent via `ClientToken`.

| API | When called | Purpose |
|---|---|---|
| `CloneDisks` | Every time the command runs. | Perform the resource operation. |
| `DescribeTasks` | When `--no-wait` is not specified. | Poll until the resource reaches the target state. (repeated) |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--category` | string | ✓ | disk category |
| `--multi-attach` | string | ✓ | multi-attach mode |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--size` | integer | ✓ | disk size in GiB |
| `--bursting-enabled` | boolean |  | enable performance burst |
| `--encrypted` | boolean |  | encrypt the disk |
| `--kms-key-id` | string |  | KMS key ID for disk encryption |
| `--name` | string |  | disk name |
| `--performance-level` | string |  | ESSD performance level |
| `--provisioned-iops` | integer |  | provisioned IOPS |
| `--resource-group` | string |  | resource group ID |
| `--tag` | key_value |  | tag assignment key=value |

## detach

```bash
ecctl ecs disk detach <id> [flags]
```

Detach disk

- Kind: `mutation` · Risk: medium
- Synchronous: waits for `Available` (waiter `available_after_detach`, timeout `300s`); use `--no-wait` to skip.

| API | When called | Purpose |
|---|---|---|
| `DetachDisk` | Every time the command runs. | Perform the resource operation. |
| `DescribeDisks` | When `--no-wait` is not specified. | Poll until the resource reaches the target state. (repeated) |
| `DescribeDisks` | When `--no-wait` is not specified. | Return the final resource view. (cached; no additional request) |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--instance` | string | ✓ | instance ID |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--delete-with-instance` | boolean |  | release the disk with the instance |

## monitor

```bash
ecctl ecs disk monitor <id> [flags]
```

Query disk monitor data

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `DescribeDiskMonitorData` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--end-time` | string | ✓ | monitoring end time |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--start-time` | string | ✓ | monitoring start time |
| `--period-seconds` | integer |  | monitoring period in seconds |

## reinit

```bash
ecctl ecs disk reinit <id> [flags]
```

Reinitialize disk

- Kind: `mutation` · Risk: medium

| API | When called | Purpose |
|---|---|---|
| `ReInitDisk` | Every time the command runs. | Perform the resource operation. |
| `DescribeDisks` | When `--no-wait` is not specified. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |

## reset

```bash
ecctl ecs disk reset <id> [flags]
```

Reset disk from snapshot

- Kind: `mutation` · Risk: medium

| API | When called | Purpose |
|---|---|---|
| `ResetDisk` | Every time the command runs. | Perform the resource operation. |
| `DescribeDisks` | When `--no-wait` is not specified. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--snapshot` | string | ✓ | snapshot ID |

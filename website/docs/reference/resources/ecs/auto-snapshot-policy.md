---
generated: true
generated_by: "website/scripts/gen-reference.mjs"
generated_command: "make build && npm --prefix website run gen:reference"
title: ecs auto-snapshot-policy
sidebar_label: auto-snapshot-policy
description: "Manage automatic snapshot policies"
---

# ecs auto-snapshot-policy

Manage automatic snapshot policies

Run `ecctl ecs auto-snapshot-policy <action> -h` for usage, or `ecctl schema ecs.auto-snapshot-policy.<action> --full` for the complete, agent-readable spec of every parameter and behavior.

## create

```bash
ecctl ecs auto-snapshot-policy create [flags]
```

Create auto snapshot policy

- Kind: `mutation` · Risk: medium

| API | When called | Purpose |
|---|---|---|
| `CreateAutoSnapshotPolicy` | Every time the command runs. | Perform the resource operation. |
| `DescribeAutoSnapshotPolicyEx` | When `--no-wait` is not specified. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--repeat-weekdays` | string | ✓ | JSON array of repeat weekdays, e.g. '["1","7"]' |
| `--retention-days` | integer | ✓ | snapshot retention days (-1 for permanent) |
| `--time-points` | string | ✓ | JSON array of snapshot time points, e.g. '["0","12"]' |
| `--copied-snapshots-retention-days` | integer |  | cross-region copied snapshot retention days |
| `--enable-cross-region-copy` | boolean |  | enable cross-region snapshot copy |
| `--name` | string |  | auto snapshot policy name |
| `--resource-group` | string |  | resource group ID |
| `--tag` | key_value |  | tag assignment key=value |
| `--target-copy-regions` | string |  | JSON array of target copy regions |

## update

```bash
ecctl ecs auto-snapshot-policy update <id> [flags]
```

Update an auto snapshot policy or its disk associations

- Kind: `mutation` · Risk: medium

| API | When called | Purpose |
|---|---|---|
| `ModifyAutoSnapshotPolicyEx` | When `--name` is specified or `--time-points` is specified or `--repeat-weekdays` is specified or `--retention-days` is specified or `--copied-snapshots-retention-days` is specified or `--enable-cross-region-copy` is specified or `--target-copy-regions` is specified. | Perform the resource operation. |
| `ApplyAutoSnapshotPolicy` | When `--attach-disk-id` is specified. | Perform the resource operation. |
| `CancelAutoSnapshotPolicy` | When `--detach-disk-id` is specified. | Perform the resource operation. |
| `DescribeAutoSnapshotPolicyEx` | When `--no-wait` is not specified. | Read the resource view. |
| `DescribeAutoSnapshotPolicyAssociations` | When `--with-associations` is specified and `--no-wait` is not specified. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--attach-disk-id` | array |  | disk IDs to apply this policy to |
| `--copied-snapshots-retention-days` | integer |  | cross-region copied snapshot retention days |
| `--detach-disk-id` | array |  | disk IDs to cancel this policy from |
| `--enable-cross-region-copy` | boolean |  | enable cross-region snapshot copy |
| `--name` | string |  | auto snapshot policy name |
| `--repeat-weekdays` | string |  | JSON array of repeat weekdays, e.g. '["1","7"]' |
| `--retention-days` | integer |  | snapshot retention days (-1 for permanent) |
| `--target-copy-regions` | string |  | JSON array of target copy regions |
| `--time-points` | string |  | JSON array of snapshot time points, e.g. '["0","12"]' |
| `--with-associations` | boolean |  | include disk association information |

## delete

```bash
ecctl ecs auto-snapshot-policy delete <id> [flags]
```

Delete auto snapshot policy

- Kind: `mutation` · Risk: high

| API | When called | Purpose |
|---|---|---|
| `DeleteAutoSnapshotPolicy` | Every time the command runs. | Perform the resource operation. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |

## get

```bash
ecctl ecs auto-snapshot-policy get <id> [flags]
```

Get auto snapshot policy

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `DescribeAutoSnapshotPolicyEx` | Every time the command runs. | Read the resource view. |
| `DescribeAutoSnapshotPolicyAssociations` | When `--with-associations` is specified. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | comma-separated resource fields to include |
| `--with-associations` | boolean |  | include disk association information |

## list

```bash
ecctl ecs auto-snapshot-policy list <id> [flags]
```

List auto snapshot policies

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `DescribeAutoSnapshotPolicyEx` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | comma-separated resource fields to include |
| `--filter` | key_value |  | filter expression key=value |
| `--limit` | integer |  | maximum resources to return (default: `100`) |
| `--page` | integer |  | results page to return (default: `1`) |

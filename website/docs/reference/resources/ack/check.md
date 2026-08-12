---
generated: true
generated_by: "website/scripts/gen-reference.mjs"
generated_command: "make build && npm --prefix website run gen:reference"
title: ack check
sidebar_label: check
description: "Manage ACK cluster check reports"
---

# ack check

Manage ACK cluster check reports

Run `ecctl ack check <action> -h` for usage, or `ecctl schema ack.check.<action> --full` for the complete, agent-readable spec of every parameter and behavior.

## create

```bash
ecctl ack check create [flags]
```

Create an ACK cluster check

- Kind: `mutation` · Risk: medium
- Synchronous: waits for `Succeeded` (waiter `succeeded_after_create`, timeout `600s`); use `--no-wait` to skip.

| API | When called | Purpose |
|---|---|---|
| `RunClusterCheck` | Every time the command runs. | Perform the resource operation. |
| `GetClusterCheck` | When `--no-wait` is not specified. | Poll until the resource reaches the target state. (repeated) |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--cluster` | string | ✓ | ACK cluster ID |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--type` | string | ✓ | check type, such as ClusterUpgrade, MasterUpgrade, NodePoolUpgrade, or ClusterMigrate |
| `--target` | string |  | check target, such as a node pool ID for NodePoolUpgrade checks |

## get

```bash
ecctl ack check get <id> [flags]
```

Get an ACK cluster check

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `GetClusterCheck` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--cluster` | string | ✓ | ACK cluster ID |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | comma-separated resource fields to include |

## list

```bash
ecctl ack check list [flags]
```

List ACK cluster checks

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `ListClusterChecks` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--cluster` | string | ✓ | ACK cluster ID |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | comma-separated resource fields to include |
| `--filter` | key_value |  | filter expression key=value |
| `--limit` | integer |  | maximum checks to return (default: `100`) |
| `--page` | integer |  | results page to return (default: `1`) |
| `--target` | string |  | check target, such as a node pool ID for NodePoolUpgrade checks |
| `--type` | string |  | check type, such as ClusterUpgrade, MasterUpgrade, NodePoolUpgrade, or ClusterMigrate |

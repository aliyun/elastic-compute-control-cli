---
title: ack node
sidebar_label: node
description: "Manage ACK cluster nodes"
---

# ack node

Manage ACK cluster nodes

Run `ecctl ack node <action> -h` for usage, or `ecctl schema ack.node.<action> --full` for the complete, agent-readable spec of every parameter and behavior.

## delete

```bash
ecctl ack node delete [<ids>...] [flags]
```

Remove nodes from an ACK cluster

- Kind: `mutation` · Risk: high
- Synchronous: waits for `absent` (waiter `absent_after_delete`, timeout `300s`); use `--no-wait` to skip.

| API | When called | Purpose |
|---|---|---|
| `DeleteClusterNodes` | Every time the command runs. | Perform the resource operation. |
| `DescribeClusterNodes` | When `--no-wait` is not specified. | Poll until the resource reaches the target state. (repeated) |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--cluster` | string | ✓ | ACK cluster ID |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--force` | boolean |  | force the delete command after caller confirmation (default: `false`) |
| `--release` | boolean |  | release ECS instances after removing nodes from the cluster (default: `false`) |

## get

```bash
ecctl ack node get <node-id> [flags]
```

Get ACK node

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `DescribeClusterNodes` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--cluster` | string | ✓ | ACK cluster ID |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | comma-separated resource fields to include |

## list

```bash
ecctl ack node list [flags]
```

List ACK nodes

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `DescribeClusterNodes` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--cluster` | string | ✓ | ACK cluster ID |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | comma-separated resource fields to include |
| `--limit` | integer |  | maximum nodes to return (default: `100`) |
| `--nodepool` | string |  | ACK node pool ID |
| `--page` | integer |  | results page to return (default: `1`) |

## attach

```bash
ecctl ack node attach [flags]
```

Attach an ECS instance directly to an ACK cluster

- Kind: `mutation` · Risk: medium
- Synchronous: waits for `Ready` (waiter `ready_after_attach`, timeout `300s`); use `--no-wait` to skip.

| API | When called | Purpose |
|---|---|---|
| `AttachInstances` | Every time the command runs. | Perform the resource operation. |
| `DescribeClusterNodes` | When `--no-wait` is not specified. | Poll until the resource reaches the target state. (repeated) |
| `DescribeClusterNodes` | When `--no-wait` is not specified. | Return the final resource view. (cached; no additional request) |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--cluster` | string | ✓ | ACK cluster ID |
| `--instance` | string | ✓ | ACK node ECS instance ID |
| `--region` | string | ✓ | Alibaba Cloud region |

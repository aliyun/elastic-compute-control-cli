---
title: lingjun cluster
sidebar_label: cluster
description: "Manage Lingjun cluster resources"
---

# lingjun cluster

Manage Lingjun cluster resources

Run `ecctl lingjun cluster <action> -h` for usage, or `ecctl schema lingjun.cluster.<action> --full` for the complete, agent-readable spec of every parameter and behavior.

## create

```bash
ecctl lingjun cluster create [flags]
```

Create a Lingjun cluster

- Kind: `mutation` · Risk: medium
- Synchronous: waits for `running` (waiter `ready_after_change`, timeout `1800s`); use `--no-wait` to skip.

| API | When called | Purpose |
|---|---|---|
| `CreateCluster` | Every time the command runs. | Perform the resource operation. |
| `DescribeCluster` | When `--no-wait` is not specified. | Poll until the resource reaches the target state. (repeated) |
| `DescribeCluster` | When `--no-wait` is not specified. | Return the final resource view. (cached; no additional request) |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--name` | string | ✓ | Lingjun cluster name |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--cluster-type` | string |  | Lingjun cluster type |
| `--components` | string |  | Components JSON array matching the Lingjun API shape |
| `--description` | string |  | Lingjun cluster description |
| `--hpn-zone` | string |  | Cluster HPN zone |
| `--ignore-failed-node-tasks` | boolean |  | Skip failed node tasks when the API supports it |
| `--networks` | string |  | Network JSON object matching the Lingjun API shape |
| `--nimiz-vswitches` | string |  | NIMIZ VSwitches JSON array |
| `--node-groups` | string |  | Node groups JSON array matching the Lingjun API shape |
| `--open-eni-jumbo-frame` | boolean |  | Enable ENI jumbo frames |
| `--resource-group` | string |  | Resource group ID |
| `--tag` | key_value |  | Tag assignment key=value |

## update

```bash
ecctl lingjun cluster update <id> [flags]
```

Extend or shrink a Lingjun cluster

- Kind: `mutation` · Risk: medium
- Synchronous: waits for `running` (waiter `ready_after_change`, timeout `1800s`); use `--no-wait` to skip.

| API | When called | Purpose |
|---|---|---|
| `ExtendCluster` | When `--extend` is specified. | Perform the resource operation. |
| `DescribeCluster` | When `--extend` is specified and `--no-wait` is not specified. | Poll until the resource reaches the target state. (repeated) |
| `ShrinkCluster` | When `--shrink` is specified. | Perform the resource operation. |
| `DescribeCluster` | When `--shrink` is specified and `--no-wait` is not specified. | Poll until the resource reaches the target state. (repeated) |
| `DescribeCluster` | When `--no-wait` is not specified. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--extend` | string |  | Node groups JSON array for cluster scale-out |
| `--ignore-failed-node-tasks` | boolean |  | Skip failed node tasks when the API supports it |
| `--ip-allocation-policy` | string |  | IP allocation policy JSON array for scale-out |
| `--shrink` | string |  | Node groups JSON array for cluster scale-in |
| `--vpd-subnets` | string |  | VPD subnet IDs JSON array for scale-out |
| `--vswitch-zone` | string |  | VSwitch zone ID for scale-out |

## delete

```bash
ecctl lingjun cluster delete <id> [flags]
```

Delete a Lingjun cluster

- Kind: `mutation` · Risk: high
- Synchronous: waits for `absent` (waiter `deleted_after_delete`, timeout `1800s`); use `--no-wait` to skip.

| API | When called | Purpose |
|---|---|---|
| `DeleteCluster` | Every time the command runs. | Perform the resource operation. |
| `DescribeCluster` | When `--no-wait` is not specified. | Poll until the resource reaches the target state. (repeated) |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |

## get

```bash
ecctl lingjun cluster get <id> [flags]
```

Get a Lingjun cluster

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `DescribeCluster` | Every time the command runs. | Read the resource view. |
| `ListClusterNodes` | When `--with-nodes` is specified. | Read the resource view. |
| `ListClusterHyperNodes` | When `--with-hyper-nodes` is specified. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | comma-separated resource fields to include |
| `--limit` | integer |  | Maximum resources to return (default: `100`) |
| `--next-token` | string |  | Next page token |
| `--resource-group` | string |  | Resource group ID |
| `--with-hyper-nodes` | boolean |  | Include cluster hyper nodes in get output |
| `--with-nodes` | boolean |  | Include cluster nodes in get output |

## list

```bash
ecctl lingjun cluster list [flags]
```

List Lingjun clusters

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `ListClusters` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | comma-separated resource fields to include |
| `--filter` | key_value |  | Filter expression key=value |
| `--limit` | integer |  | Maximum resources to return (default: `100`) |
| `--next-token` | string |  | Next page token |

---
title: ack nodepool
sidebar_label: nodepool
description: "Manage ACK nodepool resources"
---

# ack nodepool

Manage ACK nodepool resources

Run `ecctl ack nodepool <action> -h` for usage, or `ecctl schema ack.nodepool.<action> --full` for the complete, agent-readable spec of every parameter and behavior.

## create

```bash
ecctl ack nodepool create [flags]
```

Create nodepool

- Kind: `mutation` · Risk: medium
- Synchronous: waits for `active` (waiter `active_after_change`, timeout `600s`); use `--no-wait` to skip.

| API | When called | Purpose |
|---|---|---|
| `CreateClusterNodePool` | When `--config` is specified. | Perform the resource operation. |
| `DescribeClusterNodePoolDetail` | When `--config` is specified and `--no-wait` is not specified. | Poll until the resource reaches the target state. (repeated) |
| `CreateClusterNodePool` | When `--config` is not specified. | Perform the resource operation. |
| `DescribeClusterNodePoolDetail` | When `--config` is not specified and `--no-wait` is not specified. | Poll until the resource reaches the target state. (repeated) |
| `DescribeClusterNodePoolDetail` | When `--no-wait` is not specified. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--cluster` | string | ✓ | ACK cluster ID |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--config` | object |  | nodepool request body as a JSON object or @file |
| `--desired-size` | integer |  | desired node count for the nodepool |
| `--instance-type` | string_array |  | ECS instance types for the nodepool scaling group |
| `--internet-max-bandwidth-out` | integer |  | maximum outbound public bandwidth in Mbit/s for nodepool instances |
| `--name` | string |  | nodepool name |
| `--runtime` | string |  | container runtime for nodepool nodes |
| `--runtime-version` | string |  | container runtime version for nodepool nodes |
| `--system-disk-category` | string |  | system disk category for nodepool instances |
| `--system-disk-size` | integer |  | system disk size in GiB for nodepool instances |
| `--vswitch` | string_array |  | VSwitch IDs for the nodepool scaling group |

## update

```bash
ecctl ack nodepool update <id> [flags]
```

Update nodepool

- Kind: `mutation` · Risk: medium
- Synchronous: waits for `active` (waiter `active_after_change`, timeout `600s`); use `--no-wait` to skip.

| API | When called | Purpose |
|---|---|---|
| `ModifyClusterNodePool` | When `--config` is specified. | Perform the resource operation. |
| `DescribeClusterNodePoolDetail` | When `--config` is specified and `--no-wait` is not specified. | Poll until the resource reaches the target state. (repeated) |
| `ScaleClusterNodePool` | When `--desired-size` is specified. | Perform the resource operation. |
| `DescribeClusterNodePoolDetail` | When `--desired-size` is specified and `--no-wait` is not specified. | Poll until the resource reaches the target state. (repeated) |
| `ModifyNodePoolNodeConfig` | When `--with-node-config` is specified. | Perform the resource operation. |
| `DescribeClusterNodePoolDetail` | When `--with-node-config` is specified and `--no-wait` is not specified. | Poll until the resource reaches the target state. (repeated) |
| `TagResources` | When `--tag` is specified. | Perform the resource operation. |
| `UntagResources` | When `--untag` is specified. | Perform the resource operation. |
| `DescribeClusterNodePoolDetail` | When `--no-wait` is not specified. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--cluster` | string | ✓ | ACK cluster ID |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--config` | object |  | nodepool request body as a JSON object or @file |
| `--desired-size` | integer |  | desired node count for the nodepool |
| `--node-config` | object |  | node-level configuration request body as a JSON object or @file |
| `--tag` | key_value |  | Alibaba Cloud resource tag assignment key=value |
| `--untag` | string_array |  | Alibaba Cloud resource tag keys to remove |
| `--with-node-config` | boolean |  | route update to node-level configuration |

## delete

```bash
ecctl ack nodepool delete <id> [flags]
```

Delete nodepool

- Kind: `mutation` · Risk: high
- Synchronous: waits for `absent` (waiter `absent_after_delete`, timeout `600s`); use `--no-wait` to skip.

| API | When called | Purpose |
|---|---|---|
| `DeleteClusterNodepool` | Every time the command runs. | Perform the resource operation. |
| `DescribeClusterNodePools` | When `--no-wait` is not specified. | Poll until the resource reaches the target state. (repeated) |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--cluster` | string | ✓ | ACK cluster ID |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--force` | boolean |  | force deletion semantics when supported by the API (default: `false`) |

## get

```bash
ecctl ack nodepool get <id> [flags]
```

Get nodepool

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `DescribeClusterNodePoolDetail` | Every time the command runs. | Read the resource view. |
| `DescribeNodePoolVuls` | When `--with-vuls` is specified. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--cluster` | string | ✓ | ACK cluster ID |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | comma-separated resource fields to include |
| `--necessity` | string |  | vulnerability necessity filter |
| `--with-vuls` | boolean |  | include nodepool vulnerability details |

## list

```bash
ecctl ack nodepool list [flags]
```

List nodepools

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `DescribeClusterNodePools` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--cluster` | string | ✓ | ACK cluster ID |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | comma-separated resource fields to include |
| `--name` | string |  | nodepool name |

## attach

```bash
ecctl ack nodepool attach <id> [flags]
```

Attach instances to nodepool

- Kind: `mutation` · Risk: medium
- Synchronous: waits for `active` (waiter `active_after_change`, timeout `600s`); use `--no-wait` to skip.

| API | When called | Purpose |
|---|---|---|
| `AttachInstancesToNodePool` | When `--print-script-only` is not specified. | Perform the resource operation. |
| `DescribeClusterNodePoolDetail` | When `--print-script-only` is not specified and `--no-wait` is not specified. | Poll until the resource reaches the target state. (repeated) |
| `DescribeClusterAttachScripts` | When `--print-script-only` is specified. | Read the resource view. |
| `DescribeClusterNodePoolDetail` | When `--no-wait` is not specified and `--print-script-only` is not specified. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--cluster` | string | ✓ | ACK cluster ID |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--config` | object |  | nodepool request body as a JSON object or @file |
| `--instance` | string_array |  | ECS instance IDs |
| `--print-script-only` | boolean |  | print the attach script without attaching instances |

## detach

```bash
ecctl ack nodepool detach <id> [flags]
```

Detach nodes from nodepool

- Kind: `mutation` · Risk: medium
- Synchronous: waits for `active` (waiter `active_after_change`, timeout `600s`); use `--no-wait` to skip.

| API | When called | Purpose |
|---|---|---|
| `RemoveNodePoolNodes` | Every time the command runs. | Perform the resource operation. |
| `DescribeClusterNodePoolDetail` | When `--no-wait` is not specified. | Poll until the resource reaches the target state. (repeated) |
| `DescribeClusterNodePoolDetail` | When `--no-wait` is not specified. | Return the final resource view. (cached; no additional request) |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--cluster` | string | ✓ | ACK cluster ID |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--concurrency` | boolean |  | remove nodes concurrently when supported |
| `--drain-node` | boolean |  | drain nodes before removing them |
| `--force` | boolean |  | force deletion semantics when supported by the API (default: `false`) |
| `--instance` | string_array |  | ECS instance IDs |
| `--node` | string_array |  | Kubernetes node names or IDs |

## repair

```bash
ecctl ack nodepool repair <id> [flags]
```

Repair nodepool

- Kind: `mutation` · Risk: medium
- Synchronous: waits for `active` (waiter `active_after_change`, timeout `600s`); use `--no-wait` to skip.

| API | When called | Purpose |
|---|---|---|
| `RepairClusterNodePool` | When `--node` is specified or `--config` is specified or `--api-param` is specified. | Perform the resource operation. |
| `DescribeClusterNodePoolDetail` | When (`--node` is specified or `--config` is specified or `--api-param` is specified) and `--no-wait` is not specified. | Poll until the resource reaches the target state. (repeated) |
| `FixNodePoolVuls` | When `--vulnerabilities` is specified. | Perform the resource operation. |
| `DescribeClusterNodePoolDetail` | When `--vulnerabilities` is specified and `--no-wait` is not specified. | Poll until the resource reaches the target state. (repeated) |
| `DescribeClusterNodePoolDetail` | When `--no-wait` is not specified. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--cluster` | string | ✓ | ACK cluster ID |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--config` | object |  | nodepool request body as a JSON object or @file |
| `--node` | string_array |  | Kubernetes node names or IDs |
| `--vulnerabilities` | string_array |  | vulnerability IDs to fix |

## upgrade

```bash
ecctl ack nodepool upgrade <id> [flags]
```

Upgrade nodepool

- Kind: `mutation` · Risk: medium
- Synchronous: waits for `active` (waiter `active_after_change`, timeout `600s`); use `--no-wait` to skip.

| API | When called | Purpose |
|---|---|---|
| `UpgradeClusterNodepool` | Every time the command runs. | Perform the resource operation. |
| `DescribeClusterNodePoolDetail` | When `--no-wait` is not specified. | Poll until the resource reaches the target state. (repeated) |
| `DescribeClusterNodePoolDetail` | When `--no-wait` is not specified. | Return the final resource view. (cached; no additional request) |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--cluster` | string | ✓ | ACK cluster ID |
| `--config` | object | ✓ | nodepool request body as a JSON object or @file |
| `--region` | string | ✓ | Alibaba Cloud region |

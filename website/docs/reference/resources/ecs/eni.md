---
title: ecs eni
sidebar_label: eni
description: "Manage elastic network interfaces"
---

# ecs eni

Manage elastic network interfaces

Run `ecctl ecs eni <action> -h` for usage, or `ecctl schema ecs.eni.<action> --full` for the complete, agent-readable spec of every parameter and behavior.

## create

```bash
ecctl ecs eni create [flags]
```

Create ENI

- Kind: `mutation` · Risk: medium
- Synchronous: waits for `Available` (waiter `available_after_create`, timeout `300s`); use `--no-wait` to skip.
- Idempotent via `ClientToken`.

| API | When called | Purpose |
|---|---|---|
| `CreateNetworkInterface` | Every time the command runs. | Perform the resource operation. |
| `DescribeNetworkInterfaceAttribute` | When `--no-wait` is not specified. | Poll until the resource reaches the target state. (repeated) |
| `DescribeNetworkInterfaceAttribute` | When `--no-wait` is not specified. | Return the final resource view. (cached; no additional request) |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--sg` | string_array | ✓ | security group IDs |
| `--vswitch` | string | ✓ | VSwitch ID |
| `--connection-tracking` | object |  | connection tracking configuration JSON object |
| `--delete-on-release` | boolean |  | delete ENI when instance is released |
| `--description` | string |  | ENI description |
| `--enhanced-network` | object |  | enhanced network configuration JSON object |
| `--ipv4-prefix-count` | integer |  | number of IPv4 prefixes |
| `--ipv4-prefixes` | string_array |  | IPv4 prefixes |
| `--ipv6-address-count` | integer |  | number of IPv6 addresses |
| `--ipv6-addresses` | string_array |  | IPv6 addresses |
| `--ipv6-prefix-count` | integer |  | number of IPv6 prefixes |
| `--ipv6-prefixes` | string_array |  | IPv6 prefixes |
| `--name` | string |  | ENI name |
| `--primary-ip` | string |  | primary private IPv4 address |
| `--private-ip-count` | integer |  | number of secondary private IPv4 addresses |
| `--private-ips` | string_array |  | secondary private IPv4 addresses |
| `--queue-number` | integer |  | ENI queue number |
| `--queue-pair-number` | integer |  | ENI queue pair number |
| `--resource-group` | string |  | resource group ID |
| `--rx-queue-size` | integer |  | receive queue size |
| `--source-dest-check` | boolean |  | enable source/destination check |
| `--tag` | key_value |  | tag assignment key=value |
| `--traffic-config` | object |  | ENI traffic config |
| `--traffic-mode` | string |  | ENI traffic mode |
| `--tx-queue-size` | integer |  | transmit queue size |
| `--type` | string |  | ENI type |
| `--visible` | boolean |  | make the ENI visible |

## update

```bash
ecctl ecs eni update <id> [flags]
```

Update ENI

- Kind: `mutation` · Risk: medium
- Idempotent via `ClientToken`.

| API | When called | Purpose |
|---|---|---|
| `ModifyNetworkInterfaceAttribute` | When `--name` is specified or `--description` is specified or `--sg` is specified or `--queue-number` is specified or `--delete-on-release` is specified or `--rx-queue-size` is specified or `--tx-queue-size` is specified or `--source-dest-check` is specified or `--traffic-config` is specified or `--connection-tracking` is specified or `--enhanced-network` is specified. | Perform the resource operation. |
| `AssignPrivateIpAddresses` | When `--private-ip` contains a value prefixed with `+` or `--private-ip-count` is specified or `--ipv4-prefix` contains a value prefixed with `+` or `--ipv4-prefix-count` is specified. | Perform the resource operation. |
| `UnassignPrivateIpAddresses` | When `--private-ip` contains a value prefixed with `-` or `--ipv4-prefix` contains a value prefixed with `-`. | Perform the resource operation. |
| `AssignIpv6Addresses` | When `--ipv6-address` contains a value prefixed with `+` or `--ipv6-address-count` is specified or `--ipv6-prefix` contains a value prefixed with `+` or `--ipv6-prefix-count` is specified. | Perform the resource operation. |
| `UnassignIpv6Addresses` | When `--ipv6-address` contains a value prefixed with `-` or `--ipv6-prefix` contains a value prefixed with `-`. | Perform the resource operation. |
| `EnableNetworkInterfaceQoS` | When `--qos` field `status` equals `enable`. | Perform the resource operation. |
| `DisableNetworkInterfaceQoS` | When `--qos` field `status` equals `disable`. | Perform the resource operation. |
| `DescribeNetworkInterfaceAttribute` | When `--no-wait` is not specified. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--connection-tracking` | object |  | connection tracking configuration JSON object |
| `--delete-on-release` | boolean |  | delete ENI when instance is released |
| `--description` | string |  | ENI description |
| `--enhanced-network` | object |  | enhanced network configuration JSON object |
| `--ipv4-prefix` | string_array |  | IPv4 prefix changes |
| `--ipv4-prefix-count` | integer |  | number of IPv4 prefixes |
| `--ipv6-address` | string_array |  | IPv6 address changes |
| `--ipv6-address-count` | integer |  | number of IPv6 addresses |
| `--ipv6-prefix` | string_array |  | IPv6 prefix changes |
| `--ipv6-prefix-count` | integer |  | number of IPv6 prefixes |
| `--name` | string |  | ENI name |
| `--private-ip` | string_array |  | secondary private IPv4 address changes |
| `--private-ip-count` | integer |  | number of secondary private IPv4 addresses |
| `--qos` | object |  | ENI QoS speed limit settings |
| `--queue-number` | integer |  | ENI queue number |
| `--rx-queue-size` | integer |  | receive queue size |
| `--sg` | string_array |  | security group IDs |
| `--source-dest-check` | boolean |  | enable source/destination check |
| `--traffic-config` | object |  | ENI traffic config |
| `--tx-queue-size` | integer |  | transmit queue size |

## delete

```bash
ecctl ecs eni delete <id> [flags]
```

Delete ENI

- Kind: `mutation` · Risk: high
- Synchronous: waits for `absent` (waiter `deleted_after_delete`, timeout `300s`); use `--no-wait` to skip.

| API | When called | Purpose |
|---|---|---|
| `DeleteNetworkInterface` | Every time the command runs. | Perform the resource operation. |
| `DescribeNetworkInterfaces` | When `--no-wait` is not specified. | Poll until the resource reaches the target state. (repeated) |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |

## get

```bash
ecctl ecs eni get <id> [flags]
```

Get ENI

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `DescribeNetworkInterfaceAttribute` | Every time the command runs. | Read the resource view. |
| `DescribeEniMonitorData` | When `--with-monitor` is specified. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--attribute` | string |  | ENI attribute to query |
| `--end-time` | string |  | monitor end time |
| `--fields` | string |  | comma-separated resource fields to include |
| `--instance` | string |  | ECS instance ID |
| `--period` | integer |  | monitor period in seconds |
| `--start-time` | string |  | monitor start time |
| `--with-monitor` | boolean |  | include ENI monitor data |

## list

```bash
ecctl ecs eni list [<ids>...] [flags]
```

List ENIs

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `DescribeNetworkInterfaces` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | comma-separated resource fields to include |
| `--filter` | key_value |  | filter expression key=value |
| `--limit` | integer |  | maximum resources to return (default: `100`) |
| `--next-token` | string |  | token for the next result page |

## attach

```bash
ecctl ecs eni attach <id> [flags]
```

Attach ENI

- Kind: `mutation` · Risk: medium
- Synchronous: waits for `InUse` (waiter `in_use_after_attach`, timeout `300s`); use `--no-wait` to skip.

| API | When called | Purpose |
|---|---|---|
| `AttachNetworkInterface` | Every time the command runs. | Perform the resource operation. |
| `DescribeNetworkInterfaceAttribute` | When `--no-wait` is not specified. | Poll until the resource reaches the target state. (repeated) |
| `DescribeNetworkInterfaceAttribute` | When `--no-wait` is not specified. | Return the final resource view. (cached; no additional request) |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--instance` | string | ✓ | ECS instance ID |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--network-card-index` | integer |  | network card index |
| `--trunk-network-instance-id` | string |  | trunk ENI instance ID |
| `--wait-for-network-configuration-ready` | boolean |  | wait for network configuration to become ready during attach |

## detach

```bash
ecctl ecs eni detach <id> [flags]
```

Detach ENI

- Kind: `mutation` · Risk: medium
- Synchronous: waits for `Available` (waiter `available_after_detach`, timeout `300s`); use `--no-wait` to skip.

| API | When called | Purpose |
|---|---|---|
| `DetachNetworkInterface` | Every time the command runs. | Perform the resource operation. |
| `DescribeNetworkInterfaceAttribute` | When `--no-wait` is not specified. | Poll until the resource reaches the target state. (repeated) |
| `DescribeNetworkInterfaceAttribute` | When `--no-wait` is not specified. | Return the final resource view. (cached; no additional request) |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--instance` | string | ✓ | ECS instance ID |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--trunk-network-instance-id` | string |  | trunk ENI instance ID |

---
generated: true
generated_by: "website/scripts/gen-reference.mjs"
generated_command: "make build && npm --prefix website run gen:reference"
title: lingjun eni
sidebar_label: eni
description: "Manage Lingjun elastic network interfaces"
---

# lingjun eni

Manage Lingjun elastic network interfaces

Run `ecctl lingjun eni <action> -h` for usage, or `ecctl schema lingjun.eni.<action> --full` for the complete, agent-readable spec of every parameter and behavior.

## create

```bash
ecctl lingjun eni create [flags]
```

Create Lingjun ENI

- Kind: `mutation` · Risk: medium
- Synchronous: waits for `Unattached` (waiter `unattached_after_create`, timeout `300s`); use `--no-wait` to skip.
- Idempotent via `ClientToken`.

| API | When called | Purpose |
|---|---|---|
| `CreateElasticNetworkInterface` | Every time the command runs. | Perform the resource operation. |
| `GetElasticNetworkInterface` | When `--no-wait` is not specified. | Poll until the resource reaches the target state. (repeated) |
| `GetElasticNetworkInterface` | When `--no-wait` is not specified. | Return the final resource view. (cached; no additional request) |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--subnet` | string | ✓ | cloud vSwitch ID |
| `--vpd` | string | ✓ | cloud VPC ID |
| `--zone` | string | ✓ | zone ID |
| `--description` | string |  | ENI description |
| `--enable-jumbo-frame` | boolean |  | enable jumbo frame |
| `--node` | string |  | Lingjun node ID |
| `--resource-group` | string |  | resource group ID |
| `--security-group` | string |  | security group ID |
| `--tag` | key_value |  | tag assignment key=value |

## update

```bash
ecctl lingjun eni update <id> [flags]
```

Update Lingjun ENI

- Kind: `mutation` · Risk: medium
- Idempotent via `ClientToken`.

| API | When called | Purpose |
|---|---|---|
| `UpdateElasticNetworkInterface` | When `--description` is specified or `--security-group` is specified. | Perform the resource operation. |
| `AssignLeniPrivateIpAddress` | When `--ip` contains a value prefixed with `+`. | Perform the resource operation. |
| `UnassignLeniPrivateIpAddress` | When `--ip` contains a value prefixed with `-`. | Perform the resource operation. |
| `GetElasticNetworkInterface` | When `--no-wait` is not specified. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--description` | string |  | ENI description |
| `--ip` | string |  | IP change; prefix with + to assign or - to release |
| `--security-group` | string |  | security group ID |

## delete

```bash
ecctl lingjun eni delete <id> [flags]
```

Delete Lingjun ENI

- Kind: `mutation` · Risk: high
- Synchronous: waits for `absent` (waiter `deleted_after_delete`, timeout `300s`); use `--no-wait` to skip.

| API | When called | Purpose |
|---|---|---|
| `DeleteElasticNetworkInterface` | Every time the command runs. | Perform the resource operation. |
| `ListElasticNetworkInterfaces` | When `--no-wait` is not specified. | Poll until the resource reaches the target state. (repeated) |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |

## get

```bash
ecctl lingjun eni get <id> [flags]
```

Get Lingjun ENI

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `GetElasticNetworkInterface` | Every time the command runs. | Read the resource view. |
| `ListLeniPrivateIpAddresses` | When `--with-ips` is specified. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | comma-separated resource fields to include |
| `--with-ips` | boolean |  | include secondary private IP addresses |

## list

```bash
ecctl lingjun eni list [flags]
```

List Lingjun ENIs

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `ListElasticNetworkInterfaces` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | comma-separated resource fields to include |
| `--filter` | key_value |  | filter expression key=value |
| `--limit` | integer |  | maximum resources to return (default: `100`) |
| `--page` | integer |  | results page to return (default: `1`) |

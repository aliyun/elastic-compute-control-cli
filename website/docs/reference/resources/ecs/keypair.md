---
title: ecs keypair
sidebar_label: keypair
description: "Manage SSH key pairs"
---

# ecs keypair

Manage SSH key pairs

Run `ecctl ecs keypair <action> -h` for usage, or `ecctl schema ecs.keypair.<action> --full` for the complete, agent-readable spec of every parameter and behavior.

## create

```bash
ecctl ecs keypair create [flags]
```

Create or import a key pair

- Kind: `mutation` · Risk: medium

| API | When called | Purpose |
|---|---|---|
| `ImportKeyPair` | When `--public-key` is specified. | Perform the resource operation. |
| `CreateKeyPair` | When `--public-key` is not specified. | Perform the resource operation. |
| `DescribeKeyPairs` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--name` | string | ✓ | key pair name |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--public-key` | string |  | existing public key to import (switches create to ImportKeyPair) |
| `--resource-group` | string |  | resource group ID |
| `--tag` | key_value |  | tag assignment key=value |

## delete

```bash
ecctl ecs keypair delete [<ids>...] [flags]
```

Delete key pairs

- Kind: `mutation` · Risk: high

| API | When called | Purpose |
|---|---|---|
| `DeleteKeyPairs` | Every time the command runs. | Perform the resource operation. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |

## get

```bash
ecctl ecs keypair get <id> [flags]
```

Get key pair

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `DescribeKeyPairs` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | comma-separated resource fields to include |

## list

```bash
ecctl ecs keypair list <id> [flags]
```

List key pairs

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `DescribeKeyPairs` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | comma-separated resource fields to include |
| `--filter` | key_value |  | filter expression key=value |
| `--limit` | integer |  | maximum resources to return (default: `50`) |
| `--page` | integer |  | results page to return (default: `1`) |

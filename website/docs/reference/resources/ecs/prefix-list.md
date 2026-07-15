---
title: ecs prefix-list
sidebar_label: prefix-list
description: "Manage prefix lists"
---

# ecs prefix-list

Manage prefix lists

Run `ecctl ecs prefix-list <action> -h` for usage, or `ecctl schema ecs.prefix-list.<action> --full` for the complete, agent-readable spec of every parameter and behavior.

## create

```bash
ecctl ecs prefix-list create [flags]
```

Create prefix list

- Kind: `mutation` · Risk: medium
- Synchronous: waits for `matched` (waiter `entries_visible`, timeout `300s`); use `--no-wait` to skip.
- Idempotent via `ClientToken`.

| API | When called | Purpose |
|---|---|---|
| `CreatePrefixList` | Every time the command runs. | Perform the resource operation. |
| `DescribePrefixListAttributes` | When `--no-wait` is not specified and `--entry` is specified. | Poll until the resource reaches the target state. (repeated) |
| `DescribePrefixListAttributes` | When `--no-wait` is not specified. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--address-family` | string | ✓ | IP address family |
| `--max-entries` | integer | ✓ | maximum number of entries |
| `--name` | string | ✓ | prefix list name |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--description` | string |  | prefix list description |
| `--entry` | object |  | prefix list entries to create |
| `--resource-group` | string |  | resource group ID |
| `--tag` | key_value |  | tag assignment key=value |

## update

```bash
ecctl ecs prefix-list update <id> [flags]
```

Update prefix list

- Kind: `mutation` · Risk: medium
- Synchronous: waits for `matched` (waiter `entries_visible`, timeout `300s`); use `--no-wait` to skip.

| API | When called | Purpose |
|---|---|---|
| `ModifyPrefixList` | Every time the command runs. | Perform the resource operation. |
| `DescribePrefixListAttributes` | When `--no-wait` is not specified and `--entry` contains a value prefixed with `+`. | Poll until the resource reaches the target state. (repeated) |
| `DescribePrefixListAttributes` | When `--no-wait` is not specified and `--entry` contains a value prefixed with `-`. | Poll until the resource reaches the target state. (repeated) |
| `DescribePrefixListAttributes` | When `--no-wait` is not specified. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--description` | string |  | prefix list description |
| `--entry` | string |  | entry change; prefix with + to add or - to remove |
| `--name` | string |  | prefix list name |

## delete

```bash
ecctl ecs prefix-list delete <id> [flags]
```

Delete prefix list

- Kind: `mutation` · Risk: high

| API | When called | Purpose |
|---|---|---|
| `DeletePrefixList` | Every time the command runs. | Perform the resource operation. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |

## get

```bash
ecctl ecs prefix-list get <id> [flags]
```

Get prefix list

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `DescribePrefixListAttributes` | Every time the command runs. | Read the resource view. |
| `DescribePrefixListAssociations` | When `--with-associations` is specified. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | comma-separated resource fields to include |
| `--with-associations` | boolean |  | include associated resources |

## list

```bash
ecctl ecs prefix-list list [<ids>...] [flags]
```

List prefix lists

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `DescribePrefixLists` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | comma-separated resource fields to include |
| `--filter` | key_value |  | filter expression key=value |
| `--limit` | integer |  | maximum resources to return (default: `100`) |
| `--next-token` | string |  | token for the next result page |

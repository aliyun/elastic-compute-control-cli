---
generated: true
generated_by: "website/scripts/gen-reference.mjs"
generated_command: "make build && npm --prefix website run gen:reference"
title: ecs port-range-list
sidebar_label: port-range-list
description: "Manage ECS port range lists"
---

# ecs port-range-list

Manage ECS port range lists

Run `ecctl ecs port-range-list <action> -h` for usage, or `ecctl schema ecs.port-range-list.<action> --full` for the complete, agent-readable spec of every parameter and behavior.

## create

```bash
ecctl ecs port-range-list create [flags]
```

Create port range list

- Kind: `mutation` · Risk: medium
- Synchronous: waits for `matched` (waiter `entries_visible`, timeout `300s`); use `--no-wait` to skip.
- Idempotent via `ClientToken`.

| API | When called | Purpose |
|---|---|---|
| `CreatePortRangeList` | Every time the command runs. | Perform the resource operation. |
| `DescribePortRangeListEntries` | When `--no-wait` is not specified and `--entry` is specified. | Poll until the resource reaches the target state. (repeated) |
| `DescribePortRangeLists` | When `--no-wait` is not specified. | Read the resource view. |
| `DescribePortRangeListEntries` | When `--no-wait` is not specified. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--max-entries` | integer | ✓ | maximum number of entries allowed in the port range list |
| `--name` | string | ✓ | port range list name |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--description` | string |  | port range list description |
| `--entry` | object |  | port ranges to include when creating the list |
| `--resource-group` | string |  | resource group ID |
| `--tag` | key_value |  | tag assignment key=value |

## update

```bash
ecctl ecs port-range-list update <id> [flags]
```

Update port range list

- Kind: `mutation` · Risk: medium
- Synchronous: waits for `matched` (waiter `entries_visible`, timeout `300s`); use `--no-wait` to skip.
- Idempotent via `ClientToken`.

| API | When called | Purpose |
|---|---|---|
| `ModifyPortRangeList` | Every time the command runs. | Perform the resource operation. |
| `DescribePortRangeListEntries` | When `--no-wait` is not specified and `--entry` contains a value prefixed with `+`. | Poll until the resource reaches the target state. (repeated) |
| `DescribePortRangeListEntries` | When `--no-wait` is not specified and `--entry` contains a value prefixed with `-`. | Poll until the resource reaches the target state. (repeated) |
| `DescribePortRangeLists` | When `--no-wait` is not specified. | Read the resource view. |
| `DescribePortRangeListEntries` | When `--no-wait` is not specified. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--description` | string |  | port range list description |
| `--entry` | string |  | port range entry change; prefix with + to add or - to remove |
| `--name` | string |  | port range list name |

## delete

```bash
ecctl ecs port-range-list delete <id> [flags]
```

Delete port range list

- Kind: `mutation` · Risk: high

| API | When called | Purpose |
|---|---|---|
| `DeletePortRangeList` | Every time the command runs. | Perform the resource operation. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |

## get

```bash
ecctl ecs port-range-list get <id> [flags]
```

Get port range list

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `DescribePortRangeLists` | Every time the command runs. | Read the resource view. |
| `DescribePortRangeListAssociations` | When `--with-associations` is specified. | Read the resource view. |
| `DescribePortRangeListEntries` | When `--with-entries` is specified. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | comma-separated resource fields to include |
| `--with-associations` | boolean |  | include associated resources |
| `--with-entries` | boolean |  | include port range entries |

## list

```bash
ecctl ecs port-range-list list [<ids>...] [flags]
```

List port range lists

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `DescribePortRangeLists` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | comma-separated resource fields to include |
| `--filter` | key_value |  | filter expression key=value |
| `--limit` | integer |  | maximum resources to return (default: `100`) |
| `--next-token` | string |  | token for the next result page |

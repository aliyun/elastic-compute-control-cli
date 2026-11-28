---
generated: true
generated_by: "website/scripts/gen-reference.mjs"
generated_command: "make build && npm --prefix website run gen:reference"
title: sandbox quota
sidebar_label: quota
description: "Manage native FC sandbox team CPU and memory quotas."
---

# sandbox quota

Manage native FC sandbox team CPU and memory quotas.

Run `ecctl sandbox quota <action> -h` for usage, or `ecctl schema sandbox.quota.<action> --full` for the complete, agent-readable spec of every parameter and behavior.

## update

```bash
ecctl sandbox quota update <id> [flags]
```

Set a native FC sandbox team quota

- Kind: `mutation` · Risk: medium

| API | When called | Purpose |
|---|---|---|
| `UpdateQuota` | Every time the command runs. | Perform the resource operation. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--cpu-cores` | integer | ✓ | CPU quota in cores |
| `--memory-gb` | integer | ✓ | memory quota in GB |
| `--region` | string | ✓ | Alibaba Cloud region |

## delete

```bash
ecctl sandbox quota delete <id> [flags]
```

Delete a native FC sandbox team quota configuration

- Kind: `mutation` · Risk: high
- Synchronous: waits for `absent` (waiter `absent_after_delete`, timeout `300s`); use `--no-wait` to skip.

| API | When called | Purpose |
|---|---|---|
| `DeleteQuota` | Every time the command runs. | Perform the resource operation. |
| `DescribeQuota` | Every time the command runs. | Poll until the resource reaches the target state. (repeated) |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |

## get

```bash
ecctl sandbox quota get <id> [flags]
```

Get a native FC sandbox team quota

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `DescribeQuota` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | comma-separated resource fields to include |

## list

```bash
ecctl sandbox quota list [flags]
```

List native FC sandbox team quotas

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `ListQuota` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--all` | boolean |  | fetch all token pages from the beginning |
| `--fields` | string |  | comma-separated resource fields to include |
| `--limit` | integer |  | maximum quotas per page (maxResults; CLI cap 100) (default: `100`) |
| `--next-token` | string |  | server pagination token |

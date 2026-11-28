---
generated: true
generated_by: "website/scripts/gen-reference.mjs"
generated_command: "make build && npm --prefix website run gen:reference"
title: sandbox volume
sidebar_label: volume
description: "Manage native FC sandbox volume configurations."
---

# sandbox volume

Manage native FC sandbox volume configurations.

Run `ecctl sandbox volume <action> -h` for usage, or `ecctl schema sandbox.volume.<action> --full` for the complete, agent-readable spec of every parameter and behavior.

## create

```bash
ecctl sandbox volume create [flags]
```

Create a native FC sandbox volume

- Kind: `mutation` · Risk: medium
- Synchronous: waits for `AVAILABLE` (waiter `ready`, timeout `300s`); use `--no-wait` to skip.

| API | When called | Purpose |
|---|---|---|
| `CreateVolume` | Every time the command runs. | Perform the resource operation. |
| `GetVolume` | When `--no-wait` is not specified. | Poll until the resource reaches the target state. (repeated) |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--name` | string | ✓ | volume name unique within a Team |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--agentic-bucket-volume-config` | object |  | AgenticBucket volume configuration as JSON or @file |
| `--agentic-fs-volume-config` | object |  | AgenticFS volume configuration as JSON or @file |
| `--mount-config` | object |  | mount role and VPC configuration as JSON or @file |
| `--oss-volume-config` | object |  | OSS volume configuration as JSON or @file |
| `--team-id` | string |  | owning Team ID |

## update

```bash
ecctl sandbox volume update <id> [flags]
```

Update a native FC sandbox volume configuration

- Kind: `mutation` · Risk: medium

| API | When called | Purpose |
|---|---|---|
| `UpdateVolume` | Every time the command runs. | Perform the resource operation. |
| `GetVolume` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--team-id` | string | ✓ | owning Team ID |
| `--agentic-bucket-volume-config` | object |  | AgenticBucket volume configuration as JSON or @file |
| `--agentic-fs-volume-config` | object |  | AgenticFS volume configuration as JSON or @file |
| `--mount-config` | object |  | mount role and VPC configuration as JSON or @file |
| `--oss-volume-config` | object |  | OSS volume configuration as JSON or @file |

## delete

```bash
ecctl sandbox volume delete <id> [flags]
```

Delete a native FC sandbox volume configuration

- Kind: `mutation` · Risk: high
- Synchronous: waits for `absent` (waiter `absent_after_delete`, timeout `300s`); use `--no-wait` to skip.

| API | When called | Purpose |
|---|---|---|
| `DeleteVolume` | Every time the command runs. | Perform the resource operation. |
| `GetVolume` | When `--no-wait` is not specified. | Poll until the resource reaches the target state. (repeated) |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--team-id` | string |  | owning Team ID |

## get

```bash
ecctl sandbox volume get <id> [flags]
```

Get a native FC sandbox volume

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `GetVolume` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | comma-separated resource fields to include |
| `--team-id` | string |  | owning Team ID |

## list

```bash
ecctl sandbox volume list [flags]
```

List native FC sandbox volumes

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `ListVolumes` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--all` | boolean |  | fetch all token pages from the beginning |
| `--fields` | string |  | comma-separated resource fields to include |
| `--limit` | integer |  | maximum volumes per page (maxResults; CLI cap 100) (default: `100`) |
| `--name` | string |  | volume name unique within a Team |
| `--next-token` | string |  | server pagination token |
| `--resource-group` | string |  | resource group ID |
| `--status` | string |  | provider volume status |
| `--team-id` | string |  | owning Team ID |
| `--user-id` | string |  | creator user ID |

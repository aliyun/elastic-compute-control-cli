---
generated: true
generated_by: "website/scripts/gen-reference.mjs"
generated_command: "make build && npm --prefix website run gen:reference"
title: sandbox template-cache
sidebar_label: template-cache
description: "Manage native FC sandbox template image caches."
---

# sandbox template-cache

Manage native FC sandbox template image caches.

Run `ecctl sandbox template-cache <action> -h` for usage, or `ecctl schema sandbox.template-cache.<action> --full` for the complete, agent-readable spec of every parameter and behavior.

## create

```bash
ecctl sandbox template-cache create [flags]
```

Start warming a native FC template cache

- Kind: `mutation` · Risk: medium
- Synchronous: waits for `Success` (waiter `ready`, timeout `20m`); use `--no-wait` to skip.

| API | When called | Purpose |
|---|---|---|
| `CreateTemplateCache` | Every time the command runs. | Perform the resource operation. |
| `DescribeTemplateCache` | When `--no-wait` is not specified. | Poll until the resource reaches the target state. (repeated) |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--template-id` | string | ✓ | cached template ID |
| `--team-id` | string |  | owning Team ID |

## delete

```bash
ecctl sandbox template-cache delete <id> [flags]
```

Delete a native FC template cache

- Kind: `mutation` · Risk: high
- Synchronous: waits for `absent` (waiter `absent_after_delete`, timeout `300s`); use `--no-wait` to skip.

| API | When called | Purpose |
|---|---|---|
| `DeleteTemplateCache` | Every time the command runs. | Perform the resource operation. |
| `DescribeTemplateCache` | When `--no-wait` is not specified. | Poll until the resource reaches the target state. (repeated) |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--team-id` | string |  | owning Team ID |

## get

```bash
ecctl sandbox template-cache get <id> [flags]
```

Get native FC template cache status

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `DescribeTemplateCache` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | comma-separated resource fields to include |
| `--team-id` | string |  | owning Team ID |

## list

```bash
ecctl sandbox template-cache list [flags]
```

List native FC template caches

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `ListTemplateCache` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | comma-separated resource fields to include |
| `--limit` | integer |  | maximum caches per page (default: `100`) |
| `--next-token` | string |  | server pagination token |
| `--status` | string |  | cache state, separate from template build state; provider creating from Create and Describe is shown as InProgress, and ready as Success |
| `--team-id` | string |  | owning Team ID |
| `--template-id` | string |  | cached template ID |

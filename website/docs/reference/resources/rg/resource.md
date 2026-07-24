---
generated: true
generated_by: "website/scripts/gen-reference.mjs"
generated_command: "make build && npm --prefix website run gen:reference"
title: rg resource
sidebar_label: resource
description: "Manage resources across resource groups"
---

# rg resource

Manage resources across resource groups

Run `ecctl rg resource <action> -h` for usage, or `ecctl schema rg.resource.<action> --full` for the complete, agent-readable spec of every parameter and behavior.

## update

```bash
ecctl rg resource update <id> [flags]
```

Move resources to a target resource group

- Kind: `mutation` · Risk: medium
- Synchronous: waits for `matched` (waiter `resources_visible_after_move`, timeout `300s`); use `--no-wait` to skip.

| API | When called | Purpose |
|---|---|---|
| `MoveResources` | Every time the command runs. | Perform the resource operation. |
| `ListResources` | When `--no-wait` is not specified. | Poll until the resource reaches the target state. (repeated) |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--resource` | object | ✓ | resources to move |

## list

```bash
ecctl rg resource list <id> [flags]
```

List resources in a resource group

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `ListResources` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | comma-separated resource fields to include |
| `--filter` | key_value |  | filter expression key=value |
| `--limit` | integer |  | maximum resources to return (default: `100`) |
| `--page` | integer |  | results page to return (default: `1`) |

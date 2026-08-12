---
generated: true
generated_by: "website/scripts/gen-reference.mjs"
generated_command: "make build && npm --prefix website run gen:reference"
title: rg service-linked-role
sidebar_label: service-linked-role
description: "Manage service-linked roles"
---

# rg service-linked-role

Manage service-linked roles

Run `ecctl rg service-linked-role <action> -h` for usage, or `ecctl schema rg.service-linked-role.<action> --full` for the complete, agent-readable spec of every parameter and behavior.

## create

```bash
ecctl rg service-linked-role create [flags]
```

Create service-linked role

- Kind: `mutation` · Risk: medium

| API | When called | Purpose |
|---|---|---|
| `CreateServiceLinkedRole` | Every time the command runs. | Perform the resource operation. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--service-name` | string | ✓ | cloud service name (e.g. polardb.aliyuncs.com) |
| `--custom-suffix` | string |  | custom suffix for the role name |
| `--description` | string |  | role description |

## delete

```bash
ecctl rg service-linked-role delete <name> [flags]
```

Delete service-linked role

- Kind: `mutation` · Risk: high
- Synchronous: waits for `SUCCEEDED` (waiter `deleted_after_delete`, timeout `300s`); use `--no-wait` to skip.

| API | When called | Purpose |
|---|---|---|
| `DeleteServiceLinkedRole` | Every time the command runs. | Perform the resource operation. |
| `GetServiceLinkedRoleDeletionStatus` | Every time the command runs. | Poll until the resource reaches the target state. (repeated) |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |

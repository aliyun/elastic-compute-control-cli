---
generated: true
generated_by: "website/scripts/gen-reference.mjs"
generated_command: "make build && npm --prefix website run gen:reference"
title: rg notification
sidebar_label: notification
description: "Manage resource group event notifications"
---

# rg notification

Manage resource group event notifications

Run `ecctl rg notification <action> -h` for usage, or `ecctl schema rg.notification.<action> --full` for the complete, agent-readable spec of every parameter and behavior.

## get

```bash
ecctl rg notification get [flags]
```

Get resource group notification setting

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `GetResourceGroupNotificationSetting` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | comma-separated resource fields to include |

## disable

```bash
ecctl rg notification disable [flags]
```

Disable resource group notification

- Kind: `mutation` · Risk: medium

| API | When called | Purpose |
|---|---|---|
| `DisableResourceGroupNotification` | Every time the command runs. | Perform the resource operation. |
| `GetResourceGroupNotificationSetting` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |

## enable

```bash
ecctl rg notification enable [flags]
```

Enable resource group notification

- Kind: `mutation` · Risk: medium

| API | When called | Purpose |
|---|---|---|
| `EnableResourceGroupNotification` | Every time the command runs. | Perform the resource operation. |
| `GetResourceGroupNotificationSetting` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |

---
generated: true
generated_by: "website/scripts/gen-reference.mjs"
generated_command: "make build && npm --prefix website run gen:reference"
title: rg admin-setting
sidebar_label: admin-setting
description: "Manage resource group administrator settings"
---

# rg admin-setting

Manage resource group administrator settings

Run `ecctl rg admin-setting <action> -h` for usage, or `ecctl schema rg.admin-setting.<action> --full` for the complete, agent-readable spec of every parameter and behavior.

## update

```bash
ecctl rg admin-setting update [flags]
```

Update resource group admin setting

- Kind: `mutation` · Risk: medium

| API | When called | Purpose |
|---|---|---|
| `UpdateResourceGroupAdminSetting` | Every time the command runs. | Perform the resource operation. |
| `GetResourceGroupAdminSetting` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--creator-as-admin` | boolean | ✓ | whether the creator is an administrator |
| `--region` | string | ✓ | Alibaba Cloud region |

## get

```bash
ecctl rg admin-setting get [flags]
```

Get resource group admin setting

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `GetResourceGroupAdminSetting` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | comma-separated resource fields to include |

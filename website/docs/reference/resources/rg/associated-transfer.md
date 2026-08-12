---
generated: true
generated_by: "website/scripts/gen-reference.mjs"
generated_command: "make build && npm --prefix website run gen:reference"
title: rg associated-transfer
sidebar_label: associated-transfer
description: "Manage associated resource follow transfer group settings"
---

# rg associated-transfer

Manage associated resource follow transfer group settings

Run `ecctl rg associated-transfer <action> -h` for usage, or `ecctl schema rg.associated-transfer.<action> --full` for the complete, agent-readable spec of every parameter and behavior.

## update

```bash
ecctl rg associated-transfer update [flags]
```

Update associated resource transfer setting

- Kind: `mutation` · Risk: medium

| API | When called | Purpose |
|---|---|---|
| `UpdateAssociatedTransferSetting` | Every time the command runs. | Perform the resource operation. |
| `ListAssociatedTransferSetting` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--enable-existing-resources-transfer` | string |  | whether to transfer existing associated resources |
| `--rule-setting` | object |  | rule settings for associated resource transfer |
| `--status` | string |  | associated transfer status |

## list

```bash
ecctl rg associated-transfer list [flags]
```

List associated resource transfer setting

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `ListAssociatedTransferSetting` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | comma-separated resource fields to include |

## disable

```bash
ecctl rg associated-transfer disable [flags]
```

Disable associated resource transfer

- Kind: `mutation` · Risk: medium

| API | When called | Purpose |
|---|---|---|
| `DisableAssociatedTransfer` | Every time the command runs. | Perform the resource operation. |
| `ListAssociatedTransferSetting` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |

## enable

```bash
ecctl rg associated-transfer enable [flags]
```

Enable associated resource transfer

- Kind: `mutation` · Risk: medium

| API | When called | Purpose |
|---|---|---|
| `EnableAssociatedTransfer` | Every time the command runs. | Perform the resource operation. |
| `ListAssociatedTransferSetting` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |

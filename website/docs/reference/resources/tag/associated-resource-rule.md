---
generated: true
generated_by: "website/scripts/gen-reference.mjs"
generated_command: "make build && npm --prefix website run gen:reference"
title: tag associated-resource-rule
sidebar_label: associated-resource-rule
description: "Manage associated resource tag rules"
---

# tag associated-resource-rule

Manage associated resource tag rules

Run `ecctl tag associated-resource-rule <action> -h` for usage, or `ecctl schema tag.associated-resource-rule.<action> --full` for the complete, agent-readable spec of every parameter and behavior.

## create

```bash
ecctl tag associated-resource-rule create [flags]
```

Create associated resource rule

- Kind: `mutation` · Risk: medium

| API | When called | Purpose |
|---|---|---|
| `CreateAssociatedResourceRules` | Every time the command runs. | Perform the resource operation. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--setting-name` | string | ✓ | associated resource rule setting name |
| `--existing-status` | string |  | whether to apply the rule to existing associated resources |
| `--status` | string |  | associated resource rule status (default: `Enable`) |
| `--tag-keys` | string_array |  | tag keys applied by the associated resource rule |

## update

```bash
ecctl tag associated-resource-rule update <setting-name> [flags]
```

Update associated resource rule

- Kind: `mutation` · Risk: medium

| API | When called | Purpose |
|---|---|---|
| `ListAssociatedResourceRules` | When `&lt;setting-name>` is specified and (`--status` is not specified or `--existing-status` is not specified or `--tag-keys` is not specified). | Read the existing rule so omitted fields are preserved during the update. |
| `UpdateAssociatedResourceRule` | Every time the command runs. | Perform the resource operation. |
| `ListAssociatedResourceRules` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--existing-status` | string |  | whether to apply the rule to existing associated resources |
| `--status` | string |  | associated resource rule status |
| `--tag-keys` | string_array |  | tag keys applied by the associated resource rule |

## delete

```bash
ecctl tag associated-resource-rule delete <setting-name> [flags]
```

Delete associated resource rule

- Kind: `mutation` · Risk: high

| API | When called | Purpose |
|---|---|---|
| `DeleteAssociatedResourceRule` | Every time the command runs. | Perform the resource operation. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |

## list

```bash
ecctl tag associated-resource-rule list [<setting-name>...] [flags]
```

List associated resource rules

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `ListAssociatedResourceRules` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | comma-separated resource fields to include |
| `--filter` | key_value |  | filter expression key=value |
| `--limit` | integer |  | maximum rules to return (default: `100`) |
| `--next-token` | string |  | token for the next page |

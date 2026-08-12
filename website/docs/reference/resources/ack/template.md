---
generated: true
generated_by: "website/scripts/gen-reference.mjs"
generated_command: "make build && npm --prefix website run gen:reference"
title: ack template
sidebar_label: template
description: "Manage ACK orchestration templates"
---

# ack template

Manage ACK orchestration templates

Run `ecctl ack template <action> -h` for usage, or `ecctl schema ack.template.<action> --full` for the complete, agent-readable spec of every parameter and behavior.

## create

```bash
ecctl ack template create [flags]
```

Create ACK orchestration template

- Kind: `mutation` · Risk: medium

| API | When called | Purpose |
|---|---|---|
| `CreateTemplate` | Every time the command runs. | Perform the resource operation. |
| `DescribeTemplateAttribute` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--content` | string | ✓ | template content as JSON/YAML text or @file |
| `--name` | string | ✓ | template name |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--description` | string |  | template description |
| `--tags` | string |  | template tags |
| `--template-type` | string |  | template type |

## update

```bash
ecctl ack template update <id> [flags]
```

Update ACK orchestration template

- Kind: `mutation` · Risk: medium

| API | When called | Purpose |
|---|---|---|
| `UpdateTemplate` | Every time the command runs. | Perform the resource operation. |
| `DescribeTemplateAttribute` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--content` | string |  | template content as JSON/YAML text or @file |
| `--description` | string |  | template description |
| `--name` | string |  | template name |
| `--tags` | string |  | template tags |
| `--template-type` | string |  | template type |

## delete

```bash
ecctl ack template delete <id> [flags]
```

Delete ACK orchestration template

- Kind: `mutation` · Risk: high
- Synchronous: waits for `absent` (waiter `deleted_after_delete`, timeout `300s`); use `--no-wait` to skip.

| API | When called | Purpose |
|---|---|---|
| `DeleteTemplate` | Every time the command runs. | Perform the resource operation. |
| `DescribeTemplateAttribute` | When `--no-wait` is not specified. | Poll until the resource reaches the target state. (repeated) |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |

## get

```bash
ecctl ack template get <id> [flags]
```

Get ACK orchestration template

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `DescribeTemplateAttribute` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | comma-separated resource fields to include |
| `--template-type` | string |  | template type |

## list

```bash
ecctl ack template list [flags]
```

List ACK orchestration templates

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `DescribeTemplates` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | comma-separated resource fields to include |
| `--limit` | integer |  | maximum resources to return (default: `100`) |
| `--page` | integer |  | results page to return (default: `1`) |
| `--template-type` | string |  | template type |

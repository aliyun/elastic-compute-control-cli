---
generated: true
generated_by: "website/scripts/gen-reference.mjs"
generated_command: "make build && npm --prefix website run gen:reference"
title: tag resource
sidebar_label: resource
description: "Manage tags on cross-product resources"
---

# tag resource

Manage tags on cross-product resources

Run `ecctl tag resource <action> -h` for usage, or `ecctl schema tag.resource.<action> --full` for the complete, agent-readable spec of every parameter and behavior.

## list

```bash
ecctl tag resource list [flags]
```

List tags for resources or resources by tag

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `ListResourcesByTag` | When `--resource-type` is specified or `--fuzzy-type` is specified or `--include-all-tags` is specified. | Read the resource view. |
| `ListTagResources` | When `--resource-type` is not specified and `--fuzzy-type` is not specified and `--include-all-tags` is not specified. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | comma-separated resource fields to include |
| `--filter` | key_value |  | filter expression key=value |
| `--fuzzy-type` | string |  | reverse lookup matching mode |
| `--include-all-tags` | boolean |  | include all resource tags in reverse lookup results |
| `--limit` | integer |  | maximum resources to return (default: `100`) |
| `--next-token` | string |  | token for the next result page |
| `--resource-type` | string |  | resource type for tag reverse lookup |

## apply

```bash
ecctl tag resource apply [flags]
```

Apply tags to resources

- Kind: `mutation` · Risk: medium

| API | When called | Purpose |
|---|---|---|
| `TagResources` | Every time the command runs. | Perform the resource operation. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--arn` | string | ✓ | resource ARN |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--tag` | key_value | ✓ | tag assignment or filter key=value |

## remove

```bash
ecctl tag resource remove [flags]
```

Remove tags from resources

- Kind: `mutation` · Risk: high

| API | When called | Purpose |
|---|---|---|
| `UntagResources` | Every time the command runs. | Perform the resource operation. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--arn` | string | ✓ | resource ARN |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--tag-key` | string | ✓ | tag key to remove |

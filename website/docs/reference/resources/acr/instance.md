---
generated: true
generated_by: "website/scripts/gen-reference.mjs"
generated_command: "make build && npm --prefix website run gen:reference"
title: acr instance
sidebar_label: instance
description: "Query Container Registry enterprise edition instances"
---

# acr instance

Query Container Registry enterprise edition instances

Run `ecctl acr instance <action> -h` for usage, or `ecctl schema acr.instance.<action> --full` for the complete, agent-readable spec of every parameter and behavior.

## get

```bash
ecctl acr instance get <id> [flags]
```

Get an enterprise edition instance

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `GetInstance` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | comma-separated resource fields to include |

## list

```bash
ecctl acr instance list [flags]
```

List enterprise edition instances

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `ListInstance` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | comma-separated resource fields to include |
| `--filter` | key_value |  | filter expression key=value |
| `--limit` | integer |  | maximum instances to return (default: `100`) |
| `--page` | integer |  | results page to return (default: `1`) |

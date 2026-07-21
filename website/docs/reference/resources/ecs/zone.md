---
generated: true
generated_by: "website/scripts/gen-reference.mjs"
generated_command: "make build && npm --prefix website run gen:reference"
title: ecs zone
sidebar_label: zone
description: "Query ECS zones in a region"
---

# ecs zone

Query ECS zones in a region

Run `ecctl ecs zone <action> -h` for usage, or `ecctl schema ecs.zone.<action> --full` for the complete, agent-readable spec of every parameter and behavior.

## list

```bash
ecctl ecs zone list [flags]
```

List zones

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `DescribeZones` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--accept-language` | string |  | language used for localized zone names |
| `--fields` | string |  | comma-separated resource fields to include |
| `--filter` | key_value |  | filter expression key=value |
| `--verbose` | boolean |  | return the full available-resource detail for each zone |

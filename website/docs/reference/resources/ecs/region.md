---
generated: true
generated_by: "website/scripts/gen-reference.mjs"
generated_command: "make build && npm --prefix website run gen:reference"
title: ecs region
sidebar_label: region
description: "Query ECS regions"
---

# ecs region

Query ECS regions

Run `ecctl ecs region <action> -h` for usage, or `ecctl schema ecs.region.<action> --full` for the complete, agent-readable spec of every parameter and behavior.

## list

```bash
ecctl ecs region list [flags]
```

List regions

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `DescribeRegions` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--accept-language` | string |  | language used for localized region names |
| `--fields` | string |  | comma-separated resource fields to include |
| `--filter` | key_value |  | filter expression key=value |

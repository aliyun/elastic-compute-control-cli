---
title: ack region
sidebar_label: region
description: "Query ACK-supported regions"
---

# ack region

Query ACK-supported regions

Run `ecctl ack region <action> -h` for usage, or `ecctl schema ack.region.<action> --full` for the complete, agent-readable spec of every parameter and behavior.

## list

```bash
ecctl ack region list [flags]
```

List ACK-supported regions

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `DescribeRegions` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | comma-separated resource fields to include |

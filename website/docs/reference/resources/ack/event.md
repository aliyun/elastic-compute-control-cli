---
generated: true
generated_by: "website/scripts/gen-reference.mjs"
generated_command: "make build && npm --prefix website run gen:reference"
title: ack event
sidebar_label: event
description: "Query ACK control-plane events"
---

# ack event

Query ACK control-plane events

Run `ecctl ack event <action> -h` for usage, or `ecctl schema ack.event.<action> --full` for the complete, agent-readable spec of every parameter and behavior.

## list

```bash
ecctl ack event list [flags]
```

List ACK events

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `DescribeEventsForRegion` | When `--by-region` is specified. | Read the resource view. |
| `DescribeEvents` | When `--type` is specified or `--source` is specified. | Read the resource view. |
| `DescribeClusterEvents` | When `--by-region` is not specified and `--type` is not specified and `--source` is not specified. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--by-region` | boolean |  | list events across clusters in the current region |
| `--cluster` | string |  | ACK cluster ID |
| `--fields` | string |  | comma-separated resource fields to include |
| `--limit` | integer |  | maximum events to return (default: `100`) |
| `--page` | integer |  | results page to return (default: `1`) |
| `--source` | string |  | event source |
| `--type` | string |  | event type |

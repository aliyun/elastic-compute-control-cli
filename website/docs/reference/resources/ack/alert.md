---
generated: true
generated_by: "website/scripts/gen-reference.mjs"
generated_command: "make build && npm --prefix website run gen:reference"
title: ack alert
sidebar_label: alert
description: "Manage ACK alert rule state and contact-group bindings"
---

# ack alert

Manage ACK alert rule state and contact-group bindings

Run `ecctl ack alert <action> -h` for usage, or `ecctl schema ack.alert.<action> --full` for the complete, agent-readable spec of every parameter and behavior.

## update

```bash
ecctl ack alert update [flags]
```

Start or stop an ACK alert rule or ruleset

- Kind: `mutation` · Risk: medium

| API | When called | Purpose |
|---|---|---|
| `StartAlert` | When `--enabled` equals `true`. | Perform the resource operation. |
| `StopAlert` | When `--enabled` equals `false`. | Perform the resource operation. |
| `UpdateContactGroupForAlert` | When `--group-id` is specified. | Perform the resource operation. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--cluster` | string | ✓ | ACK cluster ID |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--enabled` | boolean |  | desired alert state; use --enabled=true to start or --enabled=false to stop |
| `--group-id` | string_array |  | alert contact group IDs to bind |
| `--rule-id` | string |  | alert rule ID |
| `--ruleset-id` | string |  | alert ruleset ID |

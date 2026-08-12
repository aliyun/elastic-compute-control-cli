---
generated: true
generated_by: "website/scripts/gen-reference.mjs"
generated_command: "make build && npm --prefix website run gen:reference"
title: ack inspect config
sidebar_label: inspect config
description: "Manage cluster inspection config"
---

# ack inspect config

Manage cluster inspection config

Run `ecctl ack inspect config <action> -h` for usage, or `ecctl schema ack.inspect.config.<action> --full` for the complete, agent-readable spec of every parameter and behavior.

## update

```bash
ecctl ack inspect config update [flags]
```

Update inspection config

- Kind: `mutation` · Risk: medium
- Synchronous: waits for `present` (waiter `config_present`, timeout `60s`); use `--no-wait` to skip.

| API | When called | Purpose |
|---|---|---|
| `GetClusterInspectConfig` | Every time the command runs. | Read the resource view. |
| `CreateClusterInspectConfig` | When the preceding step did not produce `existing`. | Perform the resource operation. |
| `GetClusterInspectConfig` | When the preceding step did not produce `existing`. | Poll until the resource reaches the target state. (repeated) |
| `UpdateClusterInspectConfig` | When the preceding step produced `existing`. | Perform the resource operation. |
| `GetClusterInspectConfig` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--cluster` | string | ✓ | ACK cluster ID |
| `--recurrence` | string | ✓ | RFC5545 daily recurrence rule with BYHOUR and BYMINUTE |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--disabled-check-items` | string_array |  | inspection check item names to disable |
| `--enabled` | boolean |  | enable periodic inspection |

## delete

```bash
ecctl ack inspect config delete [flags]
```

Delete inspection config

- Kind: `mutation` · Risk: high

| API | When called | Purpose |
|---|---|---|
| `DeleteClusterInspectConfig` | Every time the command runs. | Perform the resource operation. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--cluster` | string | ✓ | ACK cluster ID |
| `--region` | string | ✓ | Alibaba Cloud region |

## get

```bash
ecctl ack inspect config get [flags]
```

Get inspection config

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `GetClusterInspectConfig` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--cluster` | string | ✓ | ACK cluster ID |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | comma-separated resource fields to include |

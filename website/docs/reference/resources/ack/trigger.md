---
generated: true
generated_by: "website/scripts/gen-reference.mjs"
generated_command: "make build && npm --prefix website run gen:reference"
title: ack trigger
sidebar_label: trigger
description: "Manage ACK application redeploy triggers"
---

# ack trigger

Manage ACK application redeploy triggers

Run `ecctl ack trigger <action> -h` for usage, or `ecctl schema ack.trigger.<action> --full` for the complete, agent-readable spec of every parameter and behavior.

## create

```bash
ecctl ack trigger create [flags]
```

Create trigger

- Kind: `mutation` · Risk: medium

| API | When called | Purpose |
|---|---|---|
| `CreateTrigger` | Every time the command runs. | Perform the resource operation. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--action` | string | ✓ | trigger action |
| `--cluster` | string | ✓ | ACK cluster ID |
| `--project` | string | ✓ | trigger project in namespace/name form |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--type` | string |  | trigger type |

## delete

```bash
ecctl ack trigger delete <id> [flags]
```

Delete trigger

- Kind: `mutation` · Risk: high

| API | When called | Purpose |
|---|---|---|
| `DeleteTrigger` | Every time the command runs. | Perform the resource operation. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--cluster` | string | ✓ | ACK cluster ID |
| `--region` | string | ✓ | Alibaba Cloud region |

## get

```bash
ecctl ack trigger get <id> [flags]
```

Get trigger details

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `DescribeTrigger` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--cluster` | string | ✓ | ACK cluster ID |
| `--name` | string | ✓ | application name |
| `--namespace` | string | ✓ | application namespace |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--action` | string |  | trigger action |
| `--fields` | string |  | comma-separated resource fields to include |
| `--type` | string |  | trigger type |

## list

```bash
ecctl ack trigger list [flags]
```

List triggers for an ACK application

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `DescribeTrigger` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--cluster` | string | ✓ | ACK cluster ID |
| `--name` | string | ✓ | application name |
| `--namespace` | string | ✓ | application namespace |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--action` | string |  | trigger action |
| `--fields` | string |  | comma-separated resource fields to include |
| `--filter` | key_value |  | filter expression key=value |
| `--type` | string |  | trigger type |

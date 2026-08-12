---
generated: true
generated_by: "website/scripts/gen-reference.mjs"
generated_command: "make build && npm --prefix website run gen:reference"
title: ack policy
sidebar_label: policy
description: "Manage ACK policy catalog entries"
---

# ack policy

Manage ACK policy catalog entries

Run `ecctl ack policy <action> -h` for usage, or `ecctl schema ack.policy.<action> --full` for the complete, agent-readable spec of every parameter and behavior.

## get

```bash
ecctl ack policy get <name> [flags]
```

Get ACK policy catalog details

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `DescribePolicyDetails` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | comma-separated resource fields to include |

## list

```bash
ecctl ack policy list [flags]
```

List ACK policy catalog entries

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `DescribePolicies` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | comma-separated resource fields to include |
| `--limit` | integer |  | maximum resources to return (default: `100`) |

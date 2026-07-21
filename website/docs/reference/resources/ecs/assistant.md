---
generated: true
generated_by: "website/scripts/gen-reference.mjs"
generated_command: "make build && npm --prefix website run gen:reference"
title: ecs assistant
sidebar_label: assistant
description: "Manage Cloud Assistant service settings and agent installation"
---

# ecs assistant

Manage Cloud Assistant service settings and agent installation

Run `ecctl ecs assistant <action> -h` for usage, or `ecctl schema ecs.assistant.<action> --full` for the complete, agent-readable spec of every parameter and behavior.

## update

```bash
ecctl ecs assistant update [flags]
```

Update Cloud Assistant service settings

- Kind: `mutation` · Risk: medium

| API | When called | Purpose |
|---|---|---|
| `ModifyCloudAssistantSettings` | Every time the command runs. | Perform the resource operation. |
| `DescribeCloudAssistantSettings` | When `--no-wait` is not specified. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--setting-types` | array |  | Cloud Assistant setting types to query (DescribeCloudAssistantSettings requires at least one) (default: `["InvocationDelivery","SessionManagerDelivery","AgentUpgradeConfig","SessionManagerConfig"]`) |

## get

```bash
ecctl ecs assistant get [flags]
```

Get Cloud Assistant service settings

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `DescribeCloudAssistantSettings` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | comma-separated resource fields to include |
| `--setting-types` | array |  | Cloud Assistant setting types to query (DescribeCloudAssistantSettings requires at least one) (default: `["InvocationDelivery","SessionManagerDelivery","AgentUpgradeConfig","SessionManagerConfig"]`) |

## install

```bash
ecctl ecs assistant install [flags]
```

Install the Cloud Assistant agent on instances

- Kind: `mutation` · Risk: medium
- Synchronous: waits for `true` (waiter `assistant_available`, timeout `300s`); use `--no-wait` to skip.

| API | When called | Purpose |
|---|---|---|
| `InstallCloudAssistant` | Every time the command runs. | Perform the resource operation. |
| `DescribeCloudAssistantStatus` | When `--no-wait` is not specified. | Poll until the resource reaches the target state. (repeated) |
| `DescribeCloudAssistantStatus` | When `--no-wait` is not specified. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--instance-ids` | array | ✓ | instance IDs to install the Cloud Assistant agent on |
| `--region` | string | ✓ | Alibaba Cloud region |

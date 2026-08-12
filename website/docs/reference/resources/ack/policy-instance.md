---
generated: true
generated_by: "website/scripts/gen-reference.mjs"
generated_command: "make build && npm --prefix website run gen:reference"
title: ack policy instance
sidebar_label: policy instance
description: "Manage ACK policy instances in a cluster"
---

# ack policy instance

Manage ACK policy instances in a cluster

Run `ecctl ack policy instance <action> -h` for usage, or `ecctl schema ack.policy.instance.<action> --full` for the complete, agent-readable spec of every parameter and behavior.

## create

```bash
ecctl ack policy instance create <policy-name> [flags]
```

Create an ACK policy instance

- Kind: `mutation` · Risk: medium

| API | When called | Purpose |
|---|---|---|
| `DeployPolicyInstance` | Every time the command runs. | Perform the resource operation. |
| `DescribePolicyInstances` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--action` | string | ✓ | policy action |
| `--cluster` | string | ✓ | ACK cluster ID |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--namespaces` | array |  | namespaces the policy applies to, as a JSON array |
| `--parameters` | object |  | policy instance parameter JSON object or @file |

## update

```bash
ecctl ack policy instance update <policy-name> [flags]
```

Update an ACK policy instance

- Kind: `mutation` · Risk: medium

| API | When called | Purpose |
|---|---|---|
| `ModifyPolicyInstance` | Every time the command runs. | Perform the resource operation. |
| `DescribePolicyInstances` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--cluster` | string | ✓ | ACK cluster ID |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--action` | string |  | policy action |
| `--instance-name` | string |  | policy instance name |
| `--namespaces` | array |  | namespaces the policy applies to, as a JSON array |
| `--parameters` | object |  | policy instance parameter JSON object or @file |

## delete

```bash
ecctl ack policy instance delete <policy-name> [flags]
```

Delete an ACK policy instance

- Kind: `mutation` · Risk: high

| API | When called | Purpose |
|---|---|---|
| `DeletePolicyInstance` | Every time the command runs. | Perform the resource operation. |
| `DescribePolicyInstances` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--cluster` | string | ✓ | ACK cluster ID |
| `--instance-name` | string | ✓ | policy instance name |
| `--region` | string | ✓ | Alibaba Cloud region |

## get

```bash
ecctl ack policy instance get <policy-name> [flags]
```

Get an ACK policy instance

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `DescribePolicyInstances` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--cluster` | string | ✓ | ACK cluster ID |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | comma-separated resource fields to include |
| `--instance-name` | string |  | policy instance name |

## list

```bash
ecctl ack policy instance list [flags]
```

List ACK policy instances in a cluster

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `DescribePolicyInstances` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--cluster` | string | ✓ | ACK cluster ID |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | comma-separated resource fields to include |
| `--instance-name` | string |  | policy instance name |
| `--policy-name` | string |  | policy name |

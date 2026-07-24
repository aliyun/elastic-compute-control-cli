---
generated: true
generated_by: "website/scripts/gen-reference.mjs"
generated_command: "make build && npm --prefix website run gen:reference"
title: rg policy
sidebar_label: policy
description: "Manage resource group policies"
---

# rg policy

Manage resource group policies

Run `ecctl rg policy <action> -h` for usage, or `ecctl schema rg.policy.<action> --full` for the complete, agent-readable spec of every parameter and behavior.

## create

```bash
ecctl rg policy create [flags]
```

Create policy

- Kind: `mutation` · Risk: medium

| API | When called | Purpose |
|---|---|---|
| `CreatePolicy` | Every time the command runs. | Perform the resource operation. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--name` | string | ✓ | policy name |
| `--policy-document` | string | ✓ | policy document JSON or @file |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--description` | string |  | policy description |

## delete

```bash
ecctl rg policy delete <name> [flags]
```

Delete policy

- Kind: `mutation` · Risk: high

| API | When called | Purpose |
|---|---|---|
| `DeletePolicy` | Every time the command runs. | Perform the resource operation. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |

## get

```bash
ecctl rg policy get <name> [flags]
```

Get policy

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `GetPolicy` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--policy-type` | string | ✓ | policy type |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | comma-separated resource fields to include |
| `--language` | string |  | language for policy description |

## list

```bash
ecctl rg policy list [flags]
```

List policies

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `ListPolicies` | When `--resource-group` is not specified and `--principal-type` is not specified and `--principal-name` is not specified. | Read the resource view. |
| `ListPolicyAttachments` | When `--resource-group` is specified or `--principal-type` is specified or `--principal-name` is specified. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | comma-separated resource fields to include |
| `--filter` | key_value |  | filter expression key=value |
| `--language` | string |  | language for policy description |
| `--limit` | integer |  | maximum resources to return (default: `100`) |
| `--page` | integer |  | results page to return (default: `1`) |
| `--principal-name` | string |  | principal name |
| `--principal-type` | string |  | principal type |
| `--resource-group` | string |  | resource group ID |

## attach

```bash
ecctl rg policy attach <name> [flags]
```

Attach policy to a principal

- Kind: `mutation` · Risk: medium
- Synchronous: waits for `present` (waiter `attached_after_attach`, timeout `300s`); use `--no-wait` to skip.

| API | When called | Purpose |
|---|---|---|
| `AttachPolicy` | Every time the command runs. | Perform the resource operation. |
| `ListPolicyAttachments` | When `--no-wait` is not specified. | Poll until the resource reaches the target state. (repeated) |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--policy-type` | string | ✓ | policy type |
| `--principal-name` | string | ✓ | principal name |
| `--principal-type` | string | ✓ | principal type |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--resource-group` | string | ✓ | resource group ID |

## detach

```bash
ecctl rg policy detach <name> [flags]
```

Detach policy from a principal

- Kind: `mutation` · Risk: medium
- Synchronous: waits for `absent` (waiter `detached_after_detach`, timeout `300s`); use `--no-wait` to skip.

| API | When called | Purpose |
|---|---|---|
| `DetachPolicy` | Every time the command runs. | Perform the resource operation. |
| `ListPolicyAttachments` | When `--no-wait` is not specified. | Poll until the resource reaches the target state. (repeated) |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--policy-type` | string | ✓ | policy type |
| `--principal-name` | string | ✓ | principal name |
| `--principal-type` | string | ✓ | principal type |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--resource-group` | string | ✓ | resource group ID |

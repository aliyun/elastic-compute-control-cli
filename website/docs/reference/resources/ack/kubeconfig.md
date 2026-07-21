---
generated: true
generated_by: "website/scripts/gen-reference.mjs"
generated_command: "make build && npm --prefix website run gen:reference"
title: ack kubeconfig
sidebar_label: kubeconfig
description: "Manage ACK kubeconfig credentials"
---

# ack kubeconfig

Manage ACK kubeconfig credentials

Run `ecctl ack kubeconfig <action> -h` for usage, or `ecctl schema ack.kubeconfig.<action> --full` for the complete, agent-readable spec of every parameter and behavior.

## create

```bash
ecctl ack kubeconfig create [flags]
```

Issue kubeconfig

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `DescribeClusterUserKubeconfig` | When `--user-id` is not specified. | Read the resource view. |
| `DescribeSubaccountK8sClusterUserConfig` | When `--user-id` is specified. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--cluster` | string | ✓ | ACK cluster ID |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--expire-time` | integer |  | KubeConfig validity duration; create sends minutes, update sends hours as required by ACK |
| `--private-ip` | boolean |  | return the private endpoint kubeconfig |
| `--user-id` | string |  | RAM user or role ID for subaccount kubeconfig issuance or user-scope state queries |

## update

```bash
ecctl ack kubeconfig update [flags]
```

Update kubeconfig expiration

- Kind: `mutation` · Risk: medium

| API | When called | Purpose |
|---|---|---|
| `UpdateK8sClusterUserConfigExpire` | Every time the command runs. | Perform the resource operation. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--cluster` | string | ✓ | ACK cluster ID |
| `--expire-time` | integer | ✓ | KubeConfig validity duration; create sends minutes, update sends hours as required by ACK |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--user-id` | string | ✓ | RAM user or role ID for subaccount kubeconfig issuance or user-scope state queries |

## get

```bash
ecctl ack kubeconfig get [flags]
```

Get kubeconfig

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `DescribeClusterUserKubeconfig` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--cluster` | string | ✓ | ACK cluster ID |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | comma-separated resource fields to include |
| `--private-ip` | boolean |  | return the private endpoint kubeconfig |

## list

```bash
ecctl ack kubeconfig list [flags]
```

List kubeconfig states

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `ListClusterKubeconfigStates` | When `--scope` does not equal `user`. | Read the resource view. |
| `ListUserKubeConfigStates` | When `--scope` equals `user`. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--cluster` | string |  | ACK cluster ID |
| `--fields` | string |  | comma-separated resource fields to include |
| `--limit` | integer |  | maximum kubeconfig states to return (default: `50`) |
| `--page` | integer |  | results page to return (default: `1`) |
| `--scope` | string |  | state query scope (default: `cluster`) |
| `--user-id` | string |  | RAM user or role ID for subaccount kubeconfig issuance or user-scope state queries |

## revoke

```bash
ecctl ack kubeconfig revoke [flags]
```

Revoke kubeconfig

- Kind: `mutation` · Risk: medium

| API | When called | Purpose |
|---|---|---|
| `RevokeK8sClusterKubeConfig` | Every time the command runs. | Perform the resource operation. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--cluster` | string | ✓ | ACK cluster ID |
| `--region` | string | ✓ | Alibaba Cloud region |

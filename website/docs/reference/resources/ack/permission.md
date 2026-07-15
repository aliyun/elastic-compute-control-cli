---
title: ack permission
sidebar_label: permission
description: "Manage ACK RAM user and role permissions"
---

# ack permission

Manage ACK RAM user and role permissions

Run `ecctl ack permission <action> -h` for usage, or `ecctl schema ack.permission.<action> --full` for the complete, agent-readable spec of every parameter and behavior.

## update

```bash
ecctl ack permission update [flags]
```

Update or replace ACK RBAC permissions for a RAM user or role

- Kind: `mutation` · Risk: medium

| API | When called | Purpose |
|---|---|---|
| `UpdateUserPermissions` | When `--replace` is not specified. | Perform the resource operation. |
| `GrantPermissions` | When `--replace` is specified. | Perform the resource operation. |
| `DescribeUserPermission` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--permission` | object | ✓ | Permission assignments, one per flag as inline key=value, JSON object, or @file. |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--user-id` | string | ✓ | RAM user or RAM role ID |
| `--mode` | string |  | incremental update mode for UpdateUserPermissions (default: `patch`) |
| `--replace` | boolean |  | replace all user permissions with GrantPermissions instead of incremental UpdateUserPermissions |

## delete

```bash
ecctl ack permission delete [flags]
```

Revoke ACK RBAC permissions and kubeconfig access for a RAM user or role

- Kind: `mutation` · Risk: high

| API | When called | Purpose |
|---|---|---|
| `CleanClusterUserPermissions` | When `--all-clusters` is not specified. | Perform the resource operation. |
| `CleanUserPermissions` | When `--all-clusters` is specified. | Perform the resource operation. |
| `DescribeUserPermission` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--user-id` | string | ✓ | RAM user or RAM role ID |
| `--all-clusters` | boolean |  | clean permissions from all clusters with CleanUserPermissions |
| `--cluster` | string |  | ACK cluster ID |
| `--force` | boolean |  | force kubeconfig cleanup; defaults to false (default: `false`) |

## get

```bash
ecctl ack permission get [flags]
```

Get ACK permissions for a RAM user or role

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `DescribeUserPermission` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--user-id` | string | ✓ | RAM user or RAM role ID |
| `--fields` | string |  | comma-separated resource fields to include |

## list

```bash
ecctl ack permission list [flags]
```

List ACK permissions for a RAM user or role

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `DescribeUserPermission` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--user-id` | string | ✓ | RAM user or RAM role ID |
| `--fields` | string |  | comma-separated resource fields to include |

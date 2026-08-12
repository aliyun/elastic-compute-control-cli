---
generated: true
generated_by: "website/scripts/gen-reference.mjs"
generated_command: "make build && npm --prefix website run gen:reference"
title: ack audit
sidebar_label: audit
description: "Manage ACK cluster API Server audit log"
---

# ack audit

Manage ACK cluster API Server audit log

Run `ecctl ack audit <action> -h` for usage, or `ecctl schema ack.audit.<action> --full` for the complete, agent-readable spec of every parameter and behavior.

## update

```bash
ecctl ack audit update [flags]
```

Update ACK cluster API Server audit log

- Kind: `mutation` · Risk: medium
- Synchronous: waits for `success` (waiter `task_succeeded`, timeout `600s`); use `--no-wait` to skip.

| API | When called | Purpose |
|---|---|---|
| `UpdateClusterAuditLogConfig` | When `--enabled` equals `true`. | Perform the resource operation. |
| `DescribeTaskInfo` | When `--enabled` equals `true` and `--no-wait` is not specified. | Poll until the resource reaches the target state. (repeated) |
| `UpdateClusterAuditLogConfig` | When `--enabled` equals `false`. | Perform the resource operation. |
| `DescribeTaskInfo` | When `--enabled` equals `false` and `--no-wait` is not specified. | Poll until the resource reaches the target state. (repeated) |
| `UpdateClusterAuditLogConfig` | When `--project` is specified and `--enabled` is not explicitly specified. | Perform the resource operation. |
| `DescribeTaskInfo` | When `--project` is specified and `--enabled` is not explicitly specified and `--no-wait` is not specified. | Poll until the resource reaches the target state. (repeated) |
| `GetClusterAuditProject` | When `--no-wait` is not specified. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--cluster` | string | ✓ | ACK cluster ID |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--enabled` | boolean |  | enable or disable API Server audit logging |
| `--project` | string |  | SLS Project for API Server audit logs |

## get

```bash
ecctl ack audit get [flags]
```

Get ACK cluster API Server audit log configuration

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `GetClusterAuditProject` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--cluster` | string | ✓ | ACK cluster ID |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | comma-separated resource fields to include |

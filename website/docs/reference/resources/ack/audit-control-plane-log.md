---
generated: true
generated_by: "website/scripts/gen-reference.mjs"
generated_command: "make build && npm --prefix website run gen:reference"
title: ack audit control-plane-log
sidebar_label: audit control-plane-log
description: "Manage ACK managed cluster control plane component logs"
---

# ack audit control-plane-log

Manage ACK managed cluster control plane component logs

Run `ecctl ack audit control-plane-log <action> -h` for usage, or `ecctl schema ack.audit.control-plane-log.<action> --full` for the complete, agent-readable spec of every parameter and behavior.

## update

```bash
ecctl ack audit control-plane-log update [flags]
```

Update ACK managed cluster control plane component log

- Kind: `mutation` · Risk: medium
- Synchronous: waits for `success` (waiter `task_succeeded`, timeout `600s`); use `--no-wait` to skip.

| API | When called | Purpose |
|---|---|---|
| `UpdateControlPlaneLog` | When `--enabled` equals `false`. | Perform the resource operation. |
| `DescribeTaskInfo` | When `--enabled` equals `false` and `--no-wait` is not specified. | Poll until the resource reaches the target state. (repeated) |
| `UpdateControlPlaneLog` | When `--enabled` does not equal `false`. | Perform the resource operation. |
| `DescribeTaskInfo` | When `--enabled` does not equal `false` and `--no-wait` is not specified. | Poll until the resource reaches the target state. (repeated) |
| `CheckControlPlaneLogEnable` | When `--no-wait` is not specified. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--cluster` | string | ✓ | ACK cluster ID |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--aliuid` | string |  | Alibaba Cloud account ID |
| `--components` | string_array |  | control plane components to collect, such as apiserver,kcm,scheduler,ccm |
| `--enabled` | boolean |  | enable or disable control plane component log collection |
| `--log-ttl` | integer |  | log retention in days |
| `--project` | string |  | SLS Project for control plane component logs |

## get

```bash
ecctl ack audit control-plane-log get [flags]
```

Get ACK managed cluster control plane component log configuration

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `CheckControlPlaneLogEnable` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--cluster` | string | ✓ | ACK cluster ID |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | comma-separated resource fields to include |

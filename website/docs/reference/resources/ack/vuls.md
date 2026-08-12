---
generated: true
generated_by: "website/scripts/gen-reference.mjs"
generated_command: "make build && npm --prefix website run gen:reference"
title: ack vuls
sidebar_label: vuls
description: "Manage ACK vulnerability scans and vulnerability views"
---

# ack vuls

Manage ACK vulnerability scans and vulnerability views

Run `ecctl ack vuls <action> -h` for usage, or `ecctl schema ack.vuls.<action> --full` for the complete, agent-readable spec of every parameter and behavior.

## create

```bash
ecctl ack vuls create [flags]
```

Create an ACK vulnerability scan task

- Kind: `mutation` · Risk: medium

| API | When called | Purpose |
|---|---|---|
| `ScanClusterVuls` | Every time the command runs. | Perform the resource operation. |
| `DescribeClusterVuls` | When `--no-wait` is not specified. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--cluster` | string | ✓ | ACK cluster ID |
| `--region` | string | ✓ | Alibaba Cloud region |

## list

```bash
ecctl ack vuls list [flags]
```

List ACK vulnerabilities for a cluster or node pool

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `DescribeClusterVuls` | When `--nodepool` is not specified. | Read the resource view. |
| `DescribeNodePoolVuls` | When `--nodepool` is specified. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--cluster` | string | ✓ | ACK cluster ID |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | comma-separated resource fields to include |
| `--nodepool` | string |  | Node pool ID for nodepool-level vulnerability view |
| `--severity` | string |  | Vulnerability fix necessity for nodepool-level results |

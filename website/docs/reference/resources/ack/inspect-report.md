---
generated: true
generated_by: "website/scripts/gen-reference.mjs"
generated_command: "make build && npm --prefix website run gen:reference"
title: ack inspect report
sidebar_label: inspect report
description: "Trigger and query inspection reports"
---

# ack inspect report

Trigger and query inspection reports

Run `ecctl ack inspect report <action> -h` for usage, or `ecctl schema ack.inspect.report.<action> --full` for the complete, agent-readable spec of every parameter and behavior.

## create

```bash
ecctl ack inspect report create [flags]
```

Trigger an inspection report

- Kind: `mutation` · Risk: medium

| API | When called | Purpose |
|---|---|---|
| `RunClusterInspect` | Every time the command runs. | Perform the resource operation. |
| `ListClusterInspectReports` | When `--no-wait` is not specified. | Read the resource view. |
| `GetClusterInspectReportDetail` | When `--no-wait` is not specified. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--cluster` | string | ✓ | ACK cluster ID |
| `--region` | string | ✓ | Alibaba Cloud region |

## get

```bash
ecctl ack inspect report get <report-id> [flags]
```

Get inspection report

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `GetClusterInspectReportDetail` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--cluster` | string | ✓ | ACK cluster ID |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | comma-separated resource fields to include |

## list

```bash
ecctl ack inspect report list [flags]
```

List inspection reports

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `ListClusterInspectReports` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--cluster` | string | ✓ | ACK cluster ID |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | comma-separated resource fields to include |
| `--limit` | integer |  | maximum reports to return (default: `50`) |
| `--next-token` | string |  | pagination token returned by a previous request |

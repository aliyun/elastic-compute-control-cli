---
title: ack version
sidebar_label: version
description: "Query ACK Kubernetes version metadata"
---

# ack version

Query ACK Kubernetes version metadata

Run `ecctl ack version <action> -h` for usage, or `ecctl schema ack.version.<action> --full` for the complete, agent-readable spec of every parameter and behavior.

## list

```bash
ecctl ack version list [flags]
```

List Kubernetes version metadata

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `DescribeKubernetesVersionMetadata` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--cluster-type` | string |  | ACK cluster type to query |
| `--fields` | string |  | comma-separated resource fields to include |
| `--filter` | key_value |  | Filter expression key=value |
| `--kubernetes-version` | string |  | Kubernetes version to query |
| `--mode` | string |  | Version query mode |
| `--query-upgradable-version` | boolean |  | Query versions available for upgrade when a Kubernetes version is specified |
| `--runtime` | string |  | Container runtime used to filter supported OS images |
| `--scenario` | string |  | ACK cluster scenario profile |

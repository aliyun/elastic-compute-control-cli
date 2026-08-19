---
generated: true
generated_by: "website/scripts/gen-reference.mjs"
generated_command: "make build && npm --prefix website run gen:reference"
title: agentrun sandbox
sidebar_label: sandbox
description: "Manage isolated AgentRun sandbox instances."
---

# agentrun sandbox

Manage isolated AgentRun sandbox instances.

Run `ecctl agentrun sandbox <action> -h` for usage, or `ecctl schema agentrun.sandbox.<action> --full` for the complete, agent-readable spec of every parameter and behavior.

## create

```bash
ecctl agentrun sandbox create [flags]
```

Create an AgentRun sandbox

- Kind: `mutation` · Risk: medium
- Synchronous: waits for `READY` (waiter `ready_after_create`, timeout `600s`); use `--no-wait` to skip.

| API | When called | Purpose |
|---|---|---|
| `CreateSandbox` | Every time the command runs. | Perform the resource operation. |
| `GetSandbox` | When `--no-wait` is not specified. | Poll until the resource reaches the target state. (repeated) |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--template-name` | string | ✓ | template name |
| `--id` | string |  | sandbox ID |
| `--idle-timeout` | integer |  | sandbox idle timeout in seconds |
| `--nas-config` | object |  | NAS configuration as a JSON object or @file |
| `--oss-mount-config` | object |  | OSS mount configuration as a JSON object or @file |
| `--polar-fs-config` | object |  | PolarFS configuration as a JSON object or @file |

## delete

```bash
ecctl agentrun sandbox delete <id> [flags]
```

Delete an AgentRun sandbox

- Kind: `mutation` · Risk: high
- Synchronous: waits for `absent` (waiter `absent_after_delete`, timeout `300s`); use `--no-wait` to skip.

| API | When called | Purpose |
|---|---|---|
| `DeleteSandbox` | Every time the command runs. | Perform the resource operation. |
| `ListSandboxes` | When `--no-wait` is not specified. | Poll until the resource reaches the target state. (repeated) |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |

## get

```bash
ecctl agentrun sandbox get <id> [flags]
```

Get an AgentRun sandbox

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `GetSandbox` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | comma-separated resource fields to include |

## list

```bash
ecctl agentrun sandbox list [flags]
```

List AgentRun sandboxes

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `ListSandboxes` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | comma-separated resource fields to include |
| `--filter` | key_value |  | filter expression key=value |
| `--limit` | integer |  | maximum sandboxes to return (default: `100`) |
| `--next-token` | string |  | pagination token from a previous response |

## stop

```bash
ecctl agentrun sandbox stop <id> [flags]
```

Stop an AgentRun sandbox

- Kind: `mutation` · Risk: medium
- Synchronous: waits for `TERMINATED` (waiter `terminated_after_stop`, timeout `300s`); use `--no-wait` to skip.

| API | When called | Purpose |
|---|---|---|
| `StopSandbox` | Every time the command runs. | Perform the resource operation. |
| `GetSandbox` | When `--no-wait` is not specified. | Poll until the resource reaches the target state. (repeated) |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |

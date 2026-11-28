---
generated: true
generated_by: "website/scripts/gen-reference.mjs"
generated_command: "make build && npm --prefix website run gen:reference"
title: sandbox
sidebar_label: sandbox
description: "Manage the lifecycle and control-plane state of E2B sandboxes."
---

# sandbox

Manage the lifecycle and control-plane state of E2B sandboxes.

Run `ecctl sandbox <action> -h` for usage, or `ecctl schema sandbox.sandbox.<action> --full` for the complete, agent-readable spec of every parameter and behavior.

## create

```bash
ecctl sandbox create [flags]
```

Create an E2B sandbox

- Kind: `mutation` · Risk: medium
- Synchronous: waits for `running` (waiter `running`, timeout `300s`); use `--no-wait` to skip.

| API | When called | Purpose |
|---|---|---|
| `CreateSandbox` | Every time the command runs. | Perform the resource operation. |
| `GetSandbox` | When `--no-wait` is not specified. | Poll until the resource reaches the target state. (repeated) |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--template` | string | ✓ | template ID or alias |
| `--allow-internet-access` | boolean |  | allow sandbox internet access |
| `--auto-pause` | boolean |  | automatically pause the sandbox on timeout |
| `--auto-pause-memory` | boolean |  | preserve memory when auto-pausing |
| `--auto-resume` | object |  | automatic resume configuration as JSON or @file |
| `--env-vars` | object |  | environment variables as JSON or @file |
| `--iam` | object |  | workload identity configuration as JSON or @file |
| `--mcp` | object |  | MCP configuration as JSON or @file |
| `--metadata` | object |  | sandbox metadata as JSON or @file |
| `--network` | object |  | network configuration as JSON or @file |
| `--secure` | boolean |  | secure system communication with the sandbox |
| `--timeout-seconds` | integer |  | time to live in seconds (default: `300`) |
| `--volume-mount` | object |  | volume mounts as a JSON array or @file |

## update

```bash
ecctl sandbox update <id> [flags]
```

Update an E2B sandbox timeout or network

- Kind: `mutation` · Risk: medium

| API | When called | Purpose |
|---|---|---|
| `UpdateSandboxTimeout` | When `--timeout-seconds` is specified. | Perform the resource operation. |
| `UpdateSandboxNetwork` | When `--network` is specified. | Perform the resource operation. |
| `GetSandbox` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--network` | object |  | network configuration as JSON or @file |
| `--timeout-seconds` | integer |  | time to live in seconds |

## delete

```bash
ecctl sandbox delete <id> [flags]
```

Kill an E2B sandbox

- Kind: `mutation` · Risk: high
- Synchronous: waits for `absent` (waiter `deleted`, timeout `300s`); use `--no-wait` to skip.

| API | When called | Purpose |
|---|---|---|
| `KillSandbox` | Every time the command runs. | Perform the resource operation. |
| `GetSandbox` | When `--no-wait` is not specified. | Poll until the resource reaches the target state. (repeated) |

## get

```bash
ecctl sandbox get <id> [flags]
```

Get an E2B sandbox

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `GetSandbox` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--fields` | string |  | comma-separated resource fields to include |

## list

```bash
ecctl sandbox list [flags]
```

List E2B sandboxes

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `ListSandboxes` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--fields` | string |  | comma-separated resource fields to include |
| `--filter` | key_value |  | filter expression key=value |
| `--limit` | integer |  | maximum results to return (default: `100`) |
| `--next-token` | string |  | pagination token |

## fork

```bash
ecctl sandbox fork <id> [flags]
```

Fork an E2B sandbox

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `ForkSandbox` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--count` | integer |  | number of sandbox forks (default: `1`) |
| `--timeout-seconds` | integer |  | time to live in seconds (default: `300`) |

## logs

```bash
ecctl sandbox logs <id> [flags]
```

Read E2B sandbox logs

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `GetSandboxLogs` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--cursor` | integer |  | log cursor in milliseconds |
| `--direction` | string |  | log direction |
| `--level` | string |  | minimum log level |
| `--limit` | integer |  | maximum sandbox logs to return (default: `1000`) |
| `--search` | string |  | log message substring |

## metrics

```bash
ecctl sandbox metrics <id> [flags]
```

Read E2B sandbox metrics

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `GetSandboxMetrics` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--end` | integer |  | metrics end time in Unix seconds |
| `--start` | integer |  | metrics start time in Unix seconds |

## pause

```bash
ecctl sandbox pause <id> [flags]
```

Pause an E2B sandbox

- Kind: `mutation` · Risk: medium
- Synchronous: waits for `paused` (waiter `paused`, timeout `300s`); use `--no-wait` to skip.

| API | When called | Purpose |
|---|---|---|
| `PauseSandbox` | Every time the command runs. | Perform the resource operation. |
| `GetSandbox` | When `--no-wait` is not specified. | Poll until the resource reaches the target state. (repeated) |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--memory` | boolean |  | preserve memory when pausing (default: `true`) |

## refresh

```bash
ecctl sandbox refresh <id> [flags]
```

Extend an E2B sandbox lifetime

- Kind: `mutation` · Risk: medium

| API | When called | Purpose |
|---|---|---|
| `RefreshSandbox` | Every time the command runs. | Perform the resource operation. |
| `GetSandbox` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--duration` | integer | ✓ | refresh duration in seconds |

## resume

```bash
ecctl sandbox resume <id> [flags]
```

Resume an E2B sandbox through the control plane

- Kind: `mutation` · Risk: medium
- Synchronous: waits for `running` (waiter `running`, timeout `300s`); use `--no-wait` to skip.

| API | When called | Purpose |
|---|---|---|
| `ConnectSandbox` | Every time the command runs. | Perform the resource operation. |
| `GetSandbox` | When `--no-wait` is not specified. | Poll until the resource reaches the target state. (repeated) |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--timeout-seconds` | integer |  | time to live in seconds (default: `300`) |

## snapshot

```bash
ecctl sandbox snapshot <id> [flags]
```

Create a persistent snapshot from an E2B sandbox

- Kind: `mutation` · Risk: medium

| API | When called | Purpose |
|---|---|---|
| `CreateSandboxSnapshot` | Every time the command runs. | Perform the resource operation. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--snapshot-name` | string |  | snapshot name |

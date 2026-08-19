---
generated: true
generated_by: "website/scripts/gen-reference.mjs"
generated_command: "make build && npm --prefix website run gen:reference"
title: agentrun template
sidebar_label: template
description: "Manage AgentRun sandbox templates and their MCP service."
---

# agentrun template

Manage AgentRun sandbox templates and their MCP service.

Run `ecctl agentrun template <action> -h` for usage, or `ecctl schema agentrun.template.<action> --full` for the complete, agent-readable spec of every parameter and behavior.

## create

```bash
ecctl agentrun template create [flags]
```

Create an AgentRun sandbox template

- Kind: `mutation` · Risk: medium
- Synchronous: waits for `READY` (waiter `ready_after_change`, timeout `600s`); use `--no-wait` to skip.

| API | When called | Purpose |
|---|---|---|
| `CreateTemplate` | Every time the command runs. | Perform the resource operation. |
| `GetTemplate` | When `--no-wait` is not specified. | Poll until the resource reaches the target state. (repeated) |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--cpu` | number | ✓ | CPU cores |
| `--memory` | integer | ✓ | memory in MB |
| `--name` | string | ✓ | template name |
| `--network-configuration` | object | ✓ | network configuration as a JSON object or @file |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--template-type` | string | ✓ | template type |
| `--allow-anonymous-manage` | boolean |  | allow data-plane create, stop, and delete sandbox calls |
| `--arms-configuration` | object |  | ARMS configuration as a JSON object or @file |
| `--container-configuration` | object |  | container configuration as a JSON object or @file |
| `--credential-configuration` | object |  | credential configuration as a JSON object or @file |
| `--description` | string |  | template description |
| `--disk-size` | integer |  | disk size in MB |
| `--enable-agent` | boolean |  | enable the Sandbox Agent |
| `--enable-pre-stop` | boolean |  | enable pre-stop handling |
| `--environment-variables` | object |  | environment variables as a JSON object or @file |
| `--execution-role-arn` | string |  | execution role ARN |
| `--idle-timeout` | integer |  | sandbox idle timeout in seconds |
| `--log-configuration` | object |  | log configuration as a JSON object or @file |
| `--nas-config` | object |  | NAS mount configuration as a JSON object or @file |
| `--oss-configuration` | object |  | OSS mount configuration entries; repeat the flag with one JSON object or @file per mount |
| `--pre-stop-timeout` | integer |  | pre-stop timeout in seconds |
| `--scaling-config` | object |  | scaling configuration as a JSON object or @file |
| `--template-configuration` | object |  | template-type-specific configuration as a JSON object or @file |
| `--workspace` | string |  | workspace ID |

## update

```bash
ecctl agentrun template update <name> [flags]
```

Update an AgentRun sandbox template

- Kind: `mutation` · Risk: medium
- Synchronous: waits for `READY` (waiter `ready_after_change`, timeout `600s`); use `--no-wait` to skip.
- Idempotent via `clientToken`.

| API | When called | Purpose |
|---|---|---|
| `UpdateTemplate` | Every time the command runs. | Perform the resource operation. |
| `GetTemplate` | When `--no-wait` is not specified. | Poll until the resource reaches the target state. (repeated) |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--allow-anonymous-manage` | boolean |  | allow data-plane create, stop, and delete sandbox calls |
| `--arms-configuration` | object |  | ARMS configuration as a JSON object or @file |
| `--container-configuration` | object |  | container configuration as a JSON object or @file |
| `--cpu` | number |  | CPU cores |
| `--credential-configuration` | object |  | credential configuration as a JSON object or @file |
| `--description` | string |  | template description |
| `--enable-agent` | boolean |  | enable the Sandbox Agent |
| `--enable-pre-stop` | boolean |  | enable pre-stop handling |
| `--environment-variables` | object |  | environment variables as a JSON object or @file |
| `--execution-role-arn` | string |  | execution role ARN |
| `--idle-timeout` | integer |  | sandbox idle timeout in seconds |
| `--log-configuration` | object |  | log configuration as a JSON object or @file |
| `--memory` | integer |  | memory in MB |
| `--nas-config` | object |  | NAS mount configuration as a JSON object or @file |
| `--network-configuration` | object |  | network configuration as a JSON object or @file |
| `--oss-configuration` | object |  | OSS mount configuration entries; repeat the flag with one JSON object or @file per mount |
| `--pre-stop-timeout` | integer |  | pre-stop timeout in seconds |
| `--scaling-config` | object |  | scaling configuration as a JSON object or @file |
| `--template-configuration` | object |  | template-type-specific configuration as a JSON object or @file |
| `--workspace` | string |  | workspace ID |

## delete

```bash
ecctl agentrun template delete <name> [flags]
```

Delete an AgentRun sandbox template

- Kind: `mutation` · Risk: high
- Synchronous: waits for `absent` (waiter `absent_after_delete`, timeout `300s`); use `--no-wait` to skip.

| API | When called | Purpose |
|---|---|---|
| `DeleteTemplate` | Every time the command runs. | Perform the resource operation. |
| `ListTemplates` | When `--no-wait` is not specified. | Poll until the resource reaches the target state. (repeated) |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |

## get

```bash
ecctl agentrun template get <name> [flags]
```

Get an AgentRun sandbox template

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `GetTemplate` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | comma-separated resource fields to include |

## list

```bash
ecctl agentrun template list [flags]
```

List AgentRun sandbox templates

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `ListTemplates` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | comma-separated resource fields to include |
| `--filter` | key_value |  | filter expression key=value |
| `--limit` | integer |  | maximum templates to return (default: `100`) |
| `--page` | integer |  | results page to return (default: `1`) |
| `--workspace-ids` | string |  | workspace IDs filter accepted by AgentRun |

## disable

```bash
ecctl agentrun template disable <name> [flags]
```

Disable the MCP service for a sandbox template

- Kind: `mutation` · Risk: medium

| API | When called | Purpose |
|---|---|---|
| `StopTemplateMCP` | Every time the command runs. | Perform the resource operation. |
| `GetTemplate` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |

## enable

```bash
ecctl agentrun template enable <name> [flags]
```

Enable the MCP service for a sandbox template

- Kind: `mutation` · Risk: medium

| API | When called | Purpose |
|---|---|---|
| `ActivateTemplateMCP` | Every time the command runs. | Perform the resource operation. |
| `GetTemplate` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--enabled-tools` | array |  | MCP tools to enable |
| `--transport` | string |  | MCP transport protocol |

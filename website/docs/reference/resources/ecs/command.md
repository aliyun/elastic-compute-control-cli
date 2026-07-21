---
generated: true
generated_by: "website/scripts/gen-reference.mjs"
generated_command: "make build && npm --prefix website run gen:reference"
title: ecs command
sidebar_label: command
description: "Manage ECS Cloud Assistant command templates and invocations"
---

# ecs command

Manage ECS Cloud Assistant command templates and invocations

Run `ecctl ecs command <action> -h` for usage, or `ecctl schema ecs.command.<action> --full` for the complete, agent-readable spec of every parameter and behavior.

## create

```bash
ecctl ecs command create [flags]
```

Create cloud assistant command template

- Kind: `mutation` · Risk: medium
- Idempotent via `ClientToken`.

| API | When called | Purpose |
|---|---|---|
| `CreateCommand` | Every time the command runs. | Perform the resource operation. |
| `DescribeCommands` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--command-content` | string | ✓ | command content to run on the instance |
| `--name` | string | ✓ | command name |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--type` | string | ✓ | command type |
| `--command-timeout` | integer |  | command timeout in seconds |
| `--content-encoding` | string |  | command content encoding |
| `--description` | string |  | command description |
| `--enable-parameter` | boolean |  | enable custom parameters in the command |
| `--launcher` | string |  | command launcher |
| `--resource-group` | string |  | resource group ID |
| `--tag` | key_value |  | tag assignment key=value |
| `--working-dir` | string |  | command working directory |

## update

```bash
ecctl ecs command update <id> [flags]
```

Update command template or invocation attribute

- Kind: `mutation` · Risk: medium

| API | When called | Purpose |
|---|---|---|
| `ModifyInvocationAttribute` | When `--invocation-id` is specified. | Perform the resource operation. |
| `ModifyCommand` | When `&lt;id>` is specified. | Perform the resource operation. |
| `DescribeInvocations` | When `--invocation-id` is specified and `--no-wait` is not specified. | Read the resource view. |
| `DescribeCommands` | When `&lt;id>` is specified and `--no-wait` is not specified. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--command-content` | string |  | command content to run on the instance |
| `--command-timeout` | integer |  | command timeout in seconds |
| `--description` | string |  | command description |
| `--frequency` | string |  | scheduled execution frequency (cron expression) |
| `--invocation-id` | string |  | invocation (execution record) ID |
| `--launcher` | string |  | command launcher |
| `--name` | string |  | command name |
| `--repeat-mode` | string |  | execution repeat mode |
| `--timed` | boolean |  | whether the invocation is scheduled |
| `--working-dir` | string |  | command working directory |

## delete

```bash
ecctl ecs command delete <id> [flags]
```

Delete command template

- Kind: `mutation` · Risk: high
- Synchronous: waits for `absent` (waiter `deleted_after_delete`, timeout `120s`); use `--no-wait` to skip.

| API | When called | Purpose |
|---|---|---|
| `DeleteCommand` | Every time the command runs. | Perform the resource operation. |
| `DescribeCommands` | When `--no-wait` is not specified. | Poll until the resource reaches the target state. (repeated) |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |

## get

```bash
ecctl ecs command get <id> [flags]
```

Get command template, invocation, or invocation results

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `DescribeInvocations` | When `--invocation-id` is specified. | Read the resource view. |
| `DescribeInvocationResults` | When `--with-results` is specified. | Read the resource view. |
| `DescribeCommands` | When `&lt;id>` is specified. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | comma-separated resource fields to include |
| `--include-history` | boolean |  | include historical invocation results |
| `--include-output` | boolean |  | include invocation output in the response |
| `--instance` | string |  | single instance ID filter |
| `--invocation-id` | string |  | invocation (execution record) ID |
| `--invoke-record-status` | string |  | invocation record status filter |
| `--with-results` | boolean |  | include invocation results in the response |

## list

```bash
ecctl ecs command list <id> [flags]
```

List command templates or invocation records

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `DescribeInvocations` | When `--invocations` is specified. | Read the resource view. |
| `DescribeCommands` | When `--invocations` is not specified. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--content-encoding` | string |  | command content encoding |
| `--fields` | string |  | comma-separated resource fields to include |
| `--filter` | key_value |  | filter expression key=value |
| `--include-output` | boolean |  | include invocation output in the response |
| `--invocations` | boolean |  | list invocation records instead of command templates |
| `--limit` | integer |  | maximum resources to return (default: `50`) |
| `--next-token` | string |  | token for the next result page |

## invoke

```bash
ecctl ecs command invoke <id> [flags]
```

Invoke a command on target instances

- Kind: `mutation` · Risk: high
- Synchronous: waits for `Finished` (waiter `finished_after_invoke`, timeout `600s`); use `--no-wait` to skip.
- Idempotent via `ClientToken`.

| API | When called | Purpose |
|---|---|---|
| `InvokeCommand` | Every time the command runs. | Perform the resource operation. |
| `DescribeInvocations` | When `--no-wait` is not specified. | Poll until the resource reaches the target state. (repeated) |
| `DescribeInvocationResults` | When `--no-wait` is not specified. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--instance-ids` | array | ✓ | instance IDs to target |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--container-id` | string |  | container ID |
| `--container-name` | string |  | container name |
| `--frequency` | string |  | scheduled execution frequency (cron expression) |
| `--parameters` | string |  | command parameters as a JSON object |
| `--repeat-mode` | string |  | execution repeat mode |
| `--resource-group` | string |  | resource group ID |
| `--tag` | key_value |  | tag assignment key=value |
| `--timed` | boolean |  | whether the invocation is scheduled |
| `--username` | string |  | username to run the command as |
| `--windows-password-name` | string |  | encrypted Windows password key name |

## stop

```bash
ecctl ecs command stop <invocation-id> [flags]
```

Stop a running invocation

- Kind: `mutation` · Risk: medium
- Synchronous: waits for `Stopped` (waiter `stopped_after_stop`, timeout `300s`); use `--no-wait` to skip.

| API | When called | Purpose |
|---|---|---|
| `StopInvocation` | Every time the command runs. | Perform the resource operation. |
| `DescribeInvocations` | When `--no-wait` is not specified. | Poll until the resource reaches the target state. (repeated) |
| `DescribeInvocations` | When `--no-wait` is not specified. | Read the resource view. |
| `DescribeInvocationResults` | When `--no-wait` is not specified. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--force` | boolean |  | force stop the invocation when supported by the API (must be set explicitly) (default: `false`) |
| `--instance-ids` | array |  | instance IDs to target |

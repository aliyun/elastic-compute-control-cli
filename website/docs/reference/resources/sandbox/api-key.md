---
generated: true
generated_by: "website/scripts/gen-reference.mjs"
generated_command: "make build && npm --prefix website run gen:reference"
title: sandbox api-key
sidebar_label: api-key
description: "Manage native FC sandbox API keys."
---

# sandbox api-key

Manage native FC sandbox API keys.

Run `ecctl sandbox api-key <action> -h` for usage, or `ecctl schema sandbox.api-key.<action> --full` for the complete, agent-readable spec of every parameter and behavior.

## create

```bash
ecctl sandbox api-key create [flags]
```

Create a native FC sandbox API key

- Kind: `mutation` · Risk: medium

| API | When called | Purpose |
|---|---|---|
| `CreateApiKey` | Every time the command runs. | Perform the resource operation. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--name` | string | ✓ | API key name |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--expire-time` | string |  | expiration time |
| `--ip-blacklist` | object |  | IP blacklist rules |
| `--ip-whitelist` | object |  | IP whitelist rules |
| `--show-value` | boolean |  | reveal the plaintext API key on create or reset |
| `--team-id` | string |  | owning Team ID |

## update

```bash
ecctl sandbox api-key update <id> [flags]
```

Update a native FC sandbox API key

- Kind: `mutation` · Risk: medium

| API | When called | Purpose |
|---|---|---|
| `UpdateApiKey` | Every time the command runs. | Perform the resource operation. |
| `DescribeApiKey` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--expire-time` | string |  | expiration time |
| `--ip-blacklist` | object |  | IP blacklist rules |
| `--ip-whitelist` | object |  | IP whitelist rules |
| `--name` | string |  | API key name |
| `--status` | string |  | API key status (active or inactive) |

## delete

```bash
ecctl sandbox api-key delete <id> [flags]
```

Delete a native FC sandbox API key

- Kind: `mutation` · Risk: high
- Synchronous: waits for `absent` (waiter `absent_after_delete`, timeout `300s`); use `--no-wait` to skip.

| API | When called | Purpose |
|---|---|---|
| `DescribeApiKey` | Every time the command runs. | Read the resource view. |
| `UpdateApiKey` | When `current_api_key` field `status` from the preceding step equals `active`. | Perform the resource operation. |
| `DeleteApiKey` | Every time the command runs. | Perform the resource operation. |
| `DescribeApiKey` | When `--no-wait` is not specified. | Poll until the resource reaches the target state. (repeated) |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |

## get

```bash
ecctl sandbox api-key get <id> [flags]
```

Get a native FC sandbox API key

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `DescribeApiKey` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | comma-separated resource fields to include |

## list

```bash
ecctl sandbox api-key list [flags]
```

List native FC sandbox API keys

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `ListApiKeys` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--all` | boolean |  | fetch all pages from page 1 |
| `--fields` | string |  | comma-separated resource fields to include |
| `--limit` | integer |  | maximum API keys per page (pageSize; CLI cap 50) (default: `50`) |
| `--name` | string |  | API key name |
| `--page` | integer |  | results page to return (default: `1`) |
| `--resource-group` | string |  | resource group ID |
| `--status` | string |  | API key status (active or inactive) |
| `--team-id` | string |  | owning Team ID |
| `--user-id` | string |  | creator user ID |

## reset

```bash
ecctl sandbox api-key reset <id> [flags]
```

Reset a native FC sandbox API key

- Kind: `mutation` · Risk: medium

| API | When called | Purpose |
|---|---|---|
| `ResetApiKey` | Every time the command runs. | Perform the resource operation. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--show-value` | boolean |  | reveal the plaintext API key on create or reset |

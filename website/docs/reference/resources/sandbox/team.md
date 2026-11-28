---
generated: true
generated_by: "website/scripts/gen-reference.mjs"
generated_command: "make build && npm --prefix website run gen:reference"
title: sandbox team
sidebar_label: team
description: "Manage FC Agent Sandbox teams."
---

# sandbox team

Manage FC Agent Sandbox teams.

Run `ecctl sandbox team <action> -h` for usage, or `ecctl schema sandbox.team.<action> --full` for the complete, agent-readable spec of every parameter and behavior.

## create

```bash
ecctl sandbox team create [flags]
```

Create an FC Agent Sandbox team

- Kind: `mutation` · Risk: medium

| API | When called | Purpose |
|---|---|---|
| `CreateTeam` | Every time the command runs. | Perform the resource operation. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--name` | string | ✓ | team name |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--description` | string |  | team description |
| `--plan` | string |  | team subscription plan |
| `--resource-group` | string |  | resource group ID |

## update

```bash
ecctl sandbox team update <id> [flags]
```

Update an FC Agent Sandbox team

- Kind: `mutation` · Risk: medium

| API | When called | Purpose |
|---|---|---|
| `GetTeam` | Every time the command runs. | check read-only and team rename restrictions |
| `UpdateTeam` | Every time the command runs. | Perform the resource operation. |
| `GetTeam` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--description` | string |  | team description |
| `--name` | string |  | team name |
| `--plan` | string |  | team subscription plan |
| `--resource-group` | string |  | resource group ID |

## delete

```bash
ecctl sandbox team delete <id> [flags]
```

Delete an FC Agent Sandbox team

- Kind: `mutation` · Risk: high
- Synchronous: waits for `absent` (waiter `absent_after_delete`, timeout `300s`); use `--no-wait` to skip.

| API | When called | Purpose |
|---|---|---|
| `GetTeam` | Every time the command runs. | check whether the team is read-only |
| `DeleteTeam` | Every time the command runs. | Perform the resource operation. |
| `GetTeam` | When `--no-wait` is not specified. | Poll until the resource reaches the target state. (repeated) |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |

## get

```bash
ecctl sandbox team get <id> [flags]
```

Get an FC Agent Sandbox team

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `GetTeam` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | comma-separated resource fields to include |

## list

```bash
ecctl sandbox team list [flags]
```

List FC Agent Sandbox teams

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `ListTeams` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--all` | boolean |  | fetch all pages from page 1 |
| `--fields` | string |  | comma-separated resource fields to include |
| `--filter` | key_value |  | filter expression key=value |
| `--limit` | integer |  | maximum teams per page (default: `50`) |
| `--page` | integer |  | results page to return (default: `1`) |

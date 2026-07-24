---
generated: true
generated_by: "website/scripts/gen-reference.mjs"
generated_command: "make build && npm --prefix website run gen:reference"
title: rg role
sidebar_label: role
description: "Manage RAM roles"
---

# rg role

Manage RAM roles

Run `ecctl rg role <action> -h` for usage, or `ecctl schema rg.role.<action> --full` for the complete, agent-readable spec of every parameter and behavior.

## create

```bash
ecctl rg role create [flags]
```

Create role

- Kind: `mutation` · Risk: medium

| API | When called | Purpose |
|---|---|---|
| `CreateRole` | Every time the command runs. | Perform the resource operation. |
| `GetRole` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--assume-role-policy-document` | string | ✓ | trust policy JSON or @file |
| `--name` | string | ✓ | role name |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--description` | string |  | role description |
| `--max-session-duration` | integer |  | maximum session duration in seconds |

## update

```bash
ecctl rg role update <name> [flags]
```

Update role

- Kind: `mutation` · Risk: medium

| API | When called | Purpose |
|---|---|---|
| `UpdateRole` | Every time the command runs. | Perform the resource operation. |
| `GetRole` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--assume-role-policy-document` | string |  | trust policy JSON or @file |
| `--description` | string |  | role description |
| `--max-session-duration` | integer |  | maximum session duration in seconds |

## delete

```bash
ecctl rg role delete <name> [flags]
```

Delete role

- Kind: `mutation` · Risk: high

| API | When called | Purpose |
|---|---|---|
| `DeleteRole` | Every time the command runs. | Perform the resource operation. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |

## get

```bash
ecctl rg role get <name> [flags]
```

Get role

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `GetRole` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | comma-separated resource fields to include |
| `--language` | string |  | language for role info |

## list

```bash
ecctl rg role list [flags]
```

List roles

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `ListRoles` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | comma-separated resource fields to include |
| `--limit` | integer |  | maximum resources to return (default: `100`) |
| `--page` | integer |  | results page to return (default: `1`) |

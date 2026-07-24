---
generated: true
generated_by: "website/scripts/gen-reference.mjs"
generated_command: "make build && npm --prefix website run gen:reference"
title: rg policy version
sidebar_label: policy version
description: "Manage policy versions"
---

# rg policy version

Manage policy versions

Run `ecctl rg policy version <action> -h` for usage, or `ecctl schema rg.policy.version.<action> --full` for the complete, agent-readable spec of every parameter and behavior.

## create

```bash
ecctl rg policy version create [flags]
```

Create policy version

- Kind: `mutation` · Risk: medium

| API | When called | Purpose |
|---|---|---|
| `CreatePolicyVersion` | Every time the command runs. | Perform the resource operation. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--policy-document` | string | ✓ | policy document JSON or @file |
| `--policy-name` | string | ✓ | policy name |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--set-as-default` | boolean |  | set as the default version |

## update

```bash
ecctl rg policy version update <version-id> [flags]
```

Update policy version

- Kind: `mutation` · Risk: medium

| API | When called | Purpose |
|---|---|---|
| `SetDefaultPolicyVersion` | When `--set-as-default` is specified. | Perform the resource operation. |
| `GetPolicy` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--policy-name` | string | ✓ | policy name |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--set-as-default` | boolean |  | set as the default version |

## delete

```bash
ecctl rg policy version delete <version-id> [flags]
```

Delete policy version

- Kind: `mutation` · Risk: high

| API | When called | Purpose |
|---|---|---|
| `SetDefaultPolicyVersion` | When `--fallback-default-version` is specified. | Perform the resource operation. |
| `DeletePolicyVersion` | Every time the command runs. | Perform the resource operation. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--policy-name` | string | ✓ | policy name |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fallback-default-version` | string |  | restore this default version before deleting the requested version |

## get

```bash
ecctl rg policy version get <version-id> [flags]
```

Get policy version

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `GetPolicyVersion` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--policy-name` | string | ✓ | policy name |
| `--policy-type` | string | ✓ | policy type |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | comma-separated resource fields to include |

## list

```bash
ecctl rg policy version list [flags]
```

List policy versions

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `ListPolicyVersions` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--policy-name` | string | ✓ | policy name |
| `--policy-type` | string | ✓ | policy type |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | comma-separated resource fields to include |

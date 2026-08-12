---
generated: true
generated_by: "website/scripts/gen-reference.mjs"
generated_command: "make build && npm --prefix website run gen:reference"
title: tag policy
sidebar_label: policy
description: "Manage tag policies"
---

# tag policy

Manage tag policies

Run `ecctl tag policy <action> -h` for usage, or `ecctl schema tag.policy.<action> --full` for the complete, agent-readable spec of every parameter and behavior.

## create

```bash
ecctl tag policy create [flags]
```

Create tag policy

- Kind: `mutation` · Risk: medium
- Dry run: supported via `--dry-run`.

| API | When called | Purpose |
|---|---|---|
| `CreatePolicy` | Every time the command runs. | Perform the resource operation. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--content` | string | ✓ | tag policy document JSON or @file |
| `--name` | string | ✓ | tag policy name |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--description` | string |  | tag policy description |
| `--user-type` | string |  | tag policy mode |

## update

```bash
ecctl tag policy update <id> [flags]
```

Update tag policy

- Kind: `mutation` · Risk: medium
- Dry run: supported via `--dry-run`.

| API | When called | Purpose |
|---|---|---|
| `ModifyPolicy` | Every time the command runs. | Perform the resource operation. |
| `GetPolicy` | When `--no-wait` is not specified and `--dry-run` is not specified. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--content` | string |  | tag policy document JSON or @file |
| `--description` | string |  | tag policy description |
| `--name` | string |  | tag policy name |

## delete

```bash
ecctl tag policy delete <id> [flags]
```

Delete tag policy

- Kind: `mutation` · Risk: high

| API | When called | Purpose |
|---|---|---|
| `DeletePolicy` | Every time the command runs. | Perform the resource operation. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |

## get

```bash
ecctl tag policy get <id> [flags]
```

Get tag policy

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `GetPolicy` | Every time the command runs. | Read the resource view. |
| `GetPolicyEnableStatus` | When `--with-status` is specified. | Read the resource view. |
| `GetEffectivePolicy` | When `--with-effective` is specified. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | comma-separated resource fields to include |
| `--open-type` | string |  | enablement type for policy status |
| `--tag-keys` | array |  | tag keys for effective policy query |
| `--target` | string |  | target node ID |
| `--target-type` | string |  | target node type |
| `--user-type` | string |  | tag policy mode |
| `--with-effective` | boolean |  | include effective policy for the target |
| `--with-status` | boolean |  | include tag policy enable status |

## list

```bash
ecctl tag policy list [<ids>...] [flags]
```

List tag policies

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `ListPolicies` | When `--target` is not specified and `--target-type` is not specified and `--targets-for-policy` is not specified. | Read the resource view. |
| `ListPoliciesForTarget` | When `--target` is specified or `--target-type` is specified. | Read the resource view. |
| `ListTargetsForPolicy` | When `--targets-for-policy` is specified. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | comma-separated resource fields to include |
| `--filter` | key_value |  | filter expression key=value |
| `--limit` | integer |  | maximum resources to return (default: `100`) |
| `--next-token` | string |  | token for the next result page |
| `--target` | string |  | target node ID |
| `--target-type` | string |  | target node type |
| `--targets-for-policy` | string |  | policy ID whose attached targets should be listed |

## attach

```bash
ecctl tag policy attach <id> [flags]
```

Attach tag policy to a target

- Kind: `mutation` · Risk: medium
- Synchronous: waits for `present` (waiter `attached_after_attach`, timeout `300s`); use `--no-wait` to skip.

| API | When called | Purpose |
|---|---|---|
| `AttachPolicy` | Every time the command runs. | Perform the resource operation. |
| `ListPoliciesForTarget` | When `--no-wait` is not specified. | Poll until the resource reaches the target state. (repeated) |
| `ListPoliciesForTarget` | When `--no-wait` is not specified. | Return the final resource view. (cached; no additional request) |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--target` | string |  | target node ID |
| `--target-type` | string |  | target node type |

## detach

```bash
ecctl tag policy detach <id> [flags]
```

Detach tag policy from a target

- Kind: `mutation` · Risk: medium
- Synchronous: waits for `absent` (waiter `detached_after_detach`, timeout `300s`); use `--no-wait` to skip.

| API | When called | Purpose |
|---|---|---|
| `DetachPolicy` | Every time the command runs. | Perform the resource operation. |
| `ListPoliciesForTarget` | When `--no-wait` is not specified. | Poll until the resource reaches the target state. (repeated) |
| `ListPoliciesForTarget` | When `--no-wait` is not specified. | Return the final resource view. (cached; no additional request) |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--target` | string |  | target node ID |
| `--target-type` | string |  | target node type |

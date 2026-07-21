---
generated: true
generated_by: "website/scripts/gen-reference.mjs"
generated_command: "make build && npm --prefix website run gen:reference"
title: ecs sg
sidebar_label: sg
description: "Manage security group resources"
---

# ecs sg

Manage security group resources

Run `ecctl ecs sg <action> -h` for usage, or `ecctl schema ecs.sg.<action> --full` for the complete, agent-readable spec of every parameter and behavior.

## create

```bash
ecctl ecs sg create [flags]
```

Create security group

- Kind: `mutation` · Risk: medium
- Idempotent via `ClientToken`.

| API | When called | Purpose |
|---|---|---|
| `CreateSecurityGroup` | Every time the command runs. | Perform the resource operation. |
| `DescribeSecurityGroupAttribute` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--vpc` | string | ✓ | VPC ID |
| `--description` | string |  | security group description |
| `--name` | string |  | security group name |
| `--resource-group` | string |  | resource group ID |
| `--tag` | key_value |  | tag assignment key=value |
| `--type` | string |  | security group type (default: `normal`) |

## update

```bash
ecctl ecs sg update <id> [flags]
```

Update security group

- Kind: `mutation` · Risk: medium

| API | When called | Purpose |
|---|---|---|
| `ModifySecurityGroupAttribute` | When `--name` is specified or `--description` is specified. | Perform the resource operation. |
| `ModifySecurityGroupPolicy` | When `--inner-access-policy` is specified. | Perform the resource operation. |
| `ModifySecurityGroupRule` | When (`--rule-id` is specified or `--protocol` is specified or `--port` is specified or `--cidr` is specified or `--policy` is specified or `--priority` is specified) and `--direction` does not equal `egress`. | Perform the resource operation. |
| `ModifySecurityGroupEgressRule` | When `--direction` equals `egress` and (`--rule-id` is specified or `--protocol` is specified or `--port` is specified or `--cidr` is specified or `--policy` is specified or `--priority` is specified). | Perform the resource operation. |
| `DescribeSecurityGroupAttribute` | When `--no-wait` is not specified. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--cidr` | cidr |  | rule IPv4 CIDR block |
| `--description` | string |  | security group description |
| `--direction` | string |  | rule direction |
| `--inner-access-policy` | string |  | internal access policy |
| `--name` | string |  | security group name |
| `--policy` | string |  | rule policy |
| `--port` | string |  | rule port or port range |
| `--priority` | integer |  | rule priority |
| `--protocol` | string |  | rule protocol |
| `--rule-id` | string |  | security group rule ID |

## delete

```bash
ecctl ecs sg delete <id> [flags]
```

Delete security group

- Kind: `mutation` · Risk: high

| API | When called | Purpose |
|---|---|---|
| `DeleteSecurityGroup` | Every time the command runs. | Perform the resource operation. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |

## get

```bash
ecctl ecs sg get <id> [flags]
```

Get security group

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `DescribeSecurityGroupAttribute` | Every time the command runs. | Read the resource view. |
| `DescribeSecurityGroupReferences` | When `--with-references` is specified. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | comma-separated resource fields to include |
| `--with-references` | boolean |  | include security group references |

## list

```bash
ecctl ecs sg list [<ids>...] [flags]
```

List security groups

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `DescribeSecurityGroups` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | comma-separated resource fields to include |
| `--filter` | key_value |  | filter expression key=value |
| `--limit` | integer |  | maximum resources to return (default: `100`) |
| `--next-token` | string |  | token for the next result page |

## authorize

```bash
ecctl ecs sg authorize <id> [flags]
```

Authorize security group rules

- Kind: `mutation` · Risk: medium
- Idempotent via `ClientToken`.

| API | When called | Purpose |
|---|---|---|
| `AuthorizeSecurityGroup` | When `--direction` does not equal `egress`. | Perform the resource operation. |
| `AuthorizeSecurityGroupEgress` | When `--direction` equals `egress`. | Perform the resource operation. |
| `DescribeSecurityGroupAttribute` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--cidr` | cidr |  | rule IPv4 CIDR block |
| `--direction` | string |  | rule direction (default: `ingress`) |
| `--policy` | string |  | rule policy (default: `accept`) |
| `--port` | string |  | rule port or port range |
| `--priority` | integer |  | rule priority (default: `1`) |
| `--protocol` | string |  | rule protocol |
| `--rule` | string |  | security group rule, for example ingress:tcp:80:0.0.0.0/0 or tcp:80@0.0.0.0/0 |

## revoke

```bash
ecctl ecs sg revoke <id> [flags]
```

Revoke security group rules

- Kind: `mutation` · Risk: medium
- Idempotent via `ClientToken`.

| API | When called | Purpose |
|---|---|---|
| `RevokeSecurityGroup` | When `--direction` does not equal `egress`. | Perform the resource operation. |
| `RevokeSecurityGroupEgress` | When `--direction` equals `egress`. | Perform the resource operation. |
| `DescribeSecurityGroupAttribute` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--direction` | string |  | rule direction (default: `ingress`) |
| `--rule` | string |  | security group rule, for example ingress:tcp:80:0.0.0.0/0 or tcp:80@0.0.0.0/0 |
| `--rule-id` | string |  | security group rule ID |

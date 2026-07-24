---
generated: true
generated_by: "website/scripts/gen-reference.mjs"
generated_command: "make build && npm --prefix website run gen:reference"
title: ack
sidebar_label: ack
description: "Manage ACK clusters"
---

# ack

Manage ACK clusters

Run `ecctl ack <action> -h` for usage, or `ecctl schema ack.ack.<action> --full` for the complete, agent-readable spec of every parameter and behavior.

## create

```bash
ecctl ack create [flags]
```

Create ACK cluster

- Kind: `mutation` · Risk: medium
- Synchronous: waits for `running` (waiter `running_after_create`, timeout `1800s`); use `--no-wait` to skip.

| API | When called | Purpose |
|---|---|---|
| `CreateCluster` | Every time the command runs. | Perform the resource operation. |
| `DescribeClusterDetail` | When `--no-wait` is not specified. | Poll until the resource reaches the target state. (repeated) |
| `DescribeClusterDetail` | When `--no-wait` is not specified. | Return the final resource view. (cached; no additional request) |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--name` | string | ✓ | cluster name |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--type` | string | ✓ | cluster type |
| `--api-server-public` | boolean |  | expose the API server through a public endpoint |
| `--edition` | string |  | cluster edition or specification; common values include ack.standard, ack.pro.small, ack.pro.xlarge, ack.pro.2xlarge, ack.pro.4xlarge |
| `--pod-cidr` | cidr |  | Pod network CIDR; sent to ACK CreateCluster as container_cidr |
| `--profile` | string |  | ACK cluster scenario profile. To use both config and ACK profiles, put the config profile before the resource command, for example ecctl --profile prod ack create --profile Serverless. |
| `--resource-group` | string |  | resource group ID |
| `--service-cidr` | cidr |  | Service network CIDR |
| `--snat-entry` | boolean |  | create an SNAT entry for cluster outbound access where ACK supports it |
| `--tag` | key_value |  | tag assignment key=value |
| `--version` | string |  | target Kubernetes version |
| `--vpc` | string |  | VPC ID |
| `--vswitch` | string_array |  | VSwitch IDs for cluster networking |
| `--zone` | string_array |  | zone IDs for ACK CreateCluster zone_ids |

## update

```bash
ecctl ack update <id> [flags]
```

Update ACK cluster

- Kind: `mutation` · Risk: medium
- Synchronous: waits for `running` (waiter `running_after_update`, timeout `1800s`); use `--no-wait` to skip.

| API | When called | Purpose |
|---|---|---|
| `ModifyCluster` | When `--name` is specified or `--api-server-public` is specified or `--api-server-eip-id` is specified or `--resource-group` is specified or `--maintenance-window` is specified. | Perform the resource operation. |
| `DescribeClusterDetail` | When (`--name` is specified or `--api-server-public` is specified or `--api-server-eip-id` is specified or `--resource-group` is specified or `--maintenance-window` is specified) and `--no-wait` is not specified. | Poll until the resource reaches the target state. (repeated) |
| `MigrateCluster` | When `--to-edition` is specified. | Perform the resource operation. |
| `DescribeClusterDetail` | When `--to-edition` is specified and `--no-wait` is not specified. | Poll until the resource reaches the target state. (repeated) |
| `ModifyClusterTags` | When `--tag-replace` is specified. | Perform the resource operation. |
| `TagResources` | When `--tag` is specified. | Perform the resource operation. |
| `UntagResources` | When `--remove-tag` is specified. | Perform the resource operation. |
| `DescribeClusterDetail` | When `--no-wait` is not specified. | Read the resource view. |
| `ListTagResources` | When `--no-wait` is not specified. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--api-server-eip-id` | string |  | EIP ID to bind when exposing the API server publicly |
| `--api-server-public` | boolean |  | expose the API server through a public endpoint |
| `--maintenance-window` | object |  | maintenance window as JSON or @file using ACK field names |
| `--name` | string |  | cluster name |
| `--remove-tag` | string_array |  | tag keys to remove |
| `--resource-group` | string |  | resource group ID |
| `--tag` | key_value |  | tag assignment key=value |
| `--tag-replace` | key_value |  | complete replacement tag set key=value |
| `--to-edition` | string |  | target cluster edition for migration |

## delete

```bash
ecctl ack delete <id> [flags]
```

Delete ACK cluster

- Kind: `mutation` · Risk: high
- Synchronous: waits for `absent` (waiter `deleted_after_delete`, timeout `1800s`); use `--no-wait` to skip.

| API | When called | Purpose |
|---|---|---|
| `DeleteCluster` | Every time the command runs. | Perform the resource operation. |
| `DescribeClustersV1` | When `--no-wait` is not specified. | Poll until the resource reaches the target state. (repeated) |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--delete-options` | object |  | ACK delete_options JSON or @file |
| `--force` | boolean |  | delete cluster resources instead of retaining them (default: `false`) |
| `--retain-all-resources` | boolean |  | retain all cloud resources when deleting the cluster |
| `--retain-resources` | string_array |  | cloud resource IDs to retain during deletion |

## get

```bash
ecctl ack get <id> [flags]
```

Get ACK cluster

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `DescribeClusterDetail` | Every time the command runs. | Read the resource view. |
| `DescribeClusterResources` | When `--with-resources` is specified. | Read the resource view. |
| `ListTagResources` | When `--with-tags` is specified. | Read the resource view. |
| `DescribePolicyGovernanceInCluster` | When `--with-policy-governance` is specified. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | comma-separated resource fields to include |
| `--with-policy-governance` | boolean |  | include policy governance summary |
| `--with-resources` | boolean |  | include cloud resources associated with the cluster |
| `--with-tags` | boolean |  | include cluster tags |

## list

```bash
ecctl ack list [<ids>...] [flags]
```

List ACK clusters

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `DescribeClustersForRegion` | When `--cross-account` is specified. | Read the resource view. |
| `DescribeClustersV1` | When `--cross-account` is not specified. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--cross-account` | boolean |  | use the regional cross-account cluster list API |
| `--fields` | string |  | comma-separated resource fields to include |
| `--filter` | key_value |  | filter expression key=value |
| `--limit` | integer |  | maximum clusters to return (default: `100`) |
| `--page` | integer |  | results page to return (default: `1`) |

## upgrade

```bash
ecctl ack upgrade <id> [flags]
```

Upgrade ACK cluster

- Kind: `mutation` · Risk: medium
- Synchronous: waits for `success` (waiter `upgrade_task_succeeded`, timeout `3600s`); use `--no-wait` to skip.

| API | When called | Purpose |
|---|---|---|
| `UpgradeCluster` | Every time the command runs. | Perform the resource operation. |
| `DescribeTaskInfo` | When `--no-wait` is not specified. | Poll until the resource reaches the target state. (repeated) |
| `DescribeClusterDetail` | When `--no-wait` is not specified. | Poll until the resource reaches the target state. (repeated) |
| `DescribeClusterDetail` | When `--no-wait` is not specified. | Return the final resource view. (cached; no additional request) |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--version` | string | ✓ | target Kubernetes version |
| `--master-only` | boolean |  | upgrade master nodes only |
| `--max-parallelism` | integer |  | maximum nodes upgraded in parallel |
| `--rolling-policy` | object |  | upgrade rolling policy JSON or @file using ACK field names |

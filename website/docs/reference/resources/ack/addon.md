---
generated: true
generated_by: "website/scripts/gen-reference.mjs"
generated_command: "make build && npm --prefix website run gen:reference"
title: ack addon
sidebar_label: addon
description: "Manage ACK cluster addons"
---

# ack addon

Manage ACK cluster addons

Run `ecctl ack addon <action> -h` for usage, or `ecctl schema ack.addon.<action> --full` for the complete, agent-readable spec of every parameter and behavior.

## create

```bash
ecctl ack addon create <name> [flags]
```

Install addon on a cluster

- Kind: `mutation` · Risk: medium
- Synchronous: waits for `success` (waiter `task_succeeded`, timeout `3600s`); use `--no-wait` to skip.

| API | When called | Purpose |
|---|---|---|
| `InstallClusterAddons` | Every time the command runs. | Perform the resource operation. |
| `DescribeTaskInfo` | When `--no-wait` is not specified. | Poll until the resource reaches the target state. (repeated) |
| `DescribeClusterDetail` | When `--no-wait` is not specified. | Poll until the resource reaches the target state. (repeated) |
| `GetClusterAddonInstance` | When `--no-wait` is not specified. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--cluster` | string | ✓ | ACK cluster ID |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--version` | string | ✓ | addon version |
| `--config` | string |  | addon config as JSON/YAML text or @file |

## update

```bash
ecctl ack addon update <name> [flags]
```

Update addon config

- Kind: `mutation` · Risk: medium
- Synchronous: waits for `matched` (waiter `modify_task_visible`, timeout `600s`); use `--no-wait` to skip.

| API | When called | Purpose |
|---|---|---|
| `DescribeClusterTasks` | When `--no-wait` is not specified. | Read the resource view. |
| `ModifyClusterAddon` | Every time the command runs. | Perform the resource operation. |
| `DescribeClusterTasks` | When `--no-wait` is not specified. | Poll until the resource reaches the target state. (repeated) |
| `DescribeClusterTasks` | When `--no-wait` is not specified. | Return the final resource view. (cached; no additional request) |
| `DescribeTaskInfo` | When `--no-wait` is not specified. | Poll until the resource reaches the target state. (repeated) |
| `DescribeClusterDetail` | When `--no-wait` is not specified. | Poll until the resource reaches the target state. (repeated) |
| `GetClusterAddonInstance` | When `--no-wait` is not specified. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--cluster` | string | ✓ | ACK cluster ID |
| `--config` | string | ✓ | addon config as JSON/YAML text or @file |
| `--region` | string | ✓ | Alibaba Cloud region |

## delete

```bash
ecctl ack addon delete [<names>...] [flags]
```

Uninstall addon from a cluster

- Kind: `mutation` · Risk: high
- Synchronous: waits for `success` (waiter `task_succeeded`, timeout `3600s`); use `--no-wait` to skip.

| API | When called | Purpose |
|---|---|---|
| `UnInstallClusterAddons` | Every time the command runs. | Perform the resource operation. |
| `DescribeTaskInfo` | When `--no-wait` is not specified. | Poll until the resource reaches the target state. (repeated) |
| `DescribeClusterDetail` | When `--no-wait` is not specified. | Poll until the resource reaches the target state. (repeated) |
| `ListClusterAddonInstances` | When `--no-wait` is not specified. | Poll until the resource reaches the target state. (repeated) |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--cluster` | string | ✓ | ACK cluster ID |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--force` | boolean |  | clean up addon-related cloud resources during uninstall (default: `false`) |

## get

```bash
ecctl ack addon get <name> [flags]
```

Get addon instance or catalog metadata

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `DescribeAddon` | When `--catalog` is specified. | Read the resource view. |
| `GetClusterAddonInstance` | When `--catalog` is not specified. | Read the resource view. |
| `ListClusterAddonInstanceResources` | When `--with-resources` is specified and `--catalog` is not specified. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--catalog` | boolean |  | query the installable addon catalog |
| `--cluster` | string |  | ACK cluster ID |
| `--cluster-profile` | string |  | cluster profile filter for catalog queries |
| `--cluster-spec` | string |  | cluster spec filter for catalog queries |
| `--cluster-type` | string |  | cluster type filter for catalog queries |
| `--cluster-version` | string |  | Kubernetes version filter for catalog queries |
| `--fields` | string |  | comma-separated resource fields to include |
| `--version` | string |  | addon version |
| `--with-resources` | boolean |  | include Kubernetes resources owned by the addon instance |

## list

```bash
ecctl ack addon list [flags]
```

List addon instances or catalog metadata

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `ListAddons` | When `--catalog` is specified. | Read the resource view. |
| `ListClusterAddonInstances` | When `--catalog` is not specified. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--catalog` | boolean |  | query the installable addon catalog |
| `--cluster` | string |  | ACK cluster ID |
| `--cluster-profile` | string |  | cluster profile filter for catalog queries |
| `--cluster-spec` | string |  | cluster spec filter for catalog queries |
| `--cluster-type` | string |  | cluster type filter for catalog queries |
| `--cluster-version` | string |  | Kubernetes version filter for catalog queries |
| `--fields` | string |  | comma-separated resource fields to include |

## upgrade

```bash
ecctl ack addon upgrade <name> [flags]
```

Upgrade addon version

- Kind: `mutation` · Risk: medium
- Synchronous: waits for `success` (waiter `task_succeeded`, timeout `3600s`); use `--no-wait` to skip.

| API | When called | Purpose |
|---|---|---|
| `UpgradeClusterAddons` | Every time the command runs. | Perform the resource operation. |
| `DescribeTaskInfo` | When `--no-wait` is not specified. | Poll until the resource reaches the target state. (repeated) |
| `DescribeClusterDetail` | When `--no-wait` is not specified. | Poll until the resource reaches the target state. (repeated) |
| `GetClusterAddonInstance` | When `--no-wait` is not specified. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--cluster` | string | ✓ | ACK cluster ID |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--version` | string | ✓ | addon version |
| `--config` | string |  | addon config as JSON/YAML text or @file |
| `--policy` | string |  | addon upgrade policy |

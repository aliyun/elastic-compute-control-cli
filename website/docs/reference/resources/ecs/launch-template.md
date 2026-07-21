---
generated: true
generated_by: "website/scripts/gen-reference.mjs"
generated_command: "make build && npm --prefix website run gen:reference"
title: ecs launch-template
sidebar_label: launch-template
description: "Manage ECS launch templates"
---

# ecs launch-template

Manage ECS launch templates

Run `ecctl ecs launch-template <action> -h` for usage, or `ecctl schema ecs.launch-template.<action> --full` for the complete, agent-readable spec of every parameter and behavior.

## create

```bash
ecctl ecs launch-template create [flags]
```

Create launch template

- Kind: `mutation` · Risk: medium
- Synchronous: waits for `present` (waiter `template_visible`, timeout `300s`); use `--no-wait` to skip.

| API | When called | Purpose |
|---|---|---|
| `CreateLaunchTemplate` | Every time the command runs. | Perform the resource operation. |
| `DescribeLaunchTemplates` | When `--no-wait` is not specified. | Poll until the resource reaches the target state. (repeated) |
| `DescribeLaunchTemplates` | When `--no-wait` is not specified. | Read the resource view. |
| `DescribeLaunchTemplateVersions` | When `--no-wait` is not specified. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--name` | string | ✓ | launch template name |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--description` | string |  | instance description stored in the launch template |
| `--image` | string |  | ECS image ID |
| `--keypair` | string |  | key pair name |
| `--resource-group` | string |  | resource group ID for the launch template |
| `--resource-resource-group` | string |  | resource group ID for resources created from the template |
| `--resource-tag` | key_value |  | tag assignment key=value for resources created from the template |
| `--security-groups` | array |  | security group IDs |
| `--sg` | string |  | security group ID |
| `--tag` | key_value |  | launch template tag assignment key=value |
| `--type` | string |  | ECS instance type |
| `--version-description` | string |  | launch template version description |
| `--vswitch` | string |  | vSwitch ID |

## update

```bash
ecctl ecs launch-template update <id> [flags]
```

Create a launch template version or switch the default version

- Kind: `mutation` · Risk: medium
- Synchronous: waits for `present` (waiter `created_version_visible`, timeout `300s`); use `--no-wait` to skip.

| API | When called | Purpose |
|---|---|---|
| `CreateLaunchTemplateVersion` | When `--create-version` is specified. | Perform the resource operation. |
| `DescribeLaunchTemplateVersions` | When `--create-version` is specified and `--no-wait` is not specified. | Poll until the resource reaches the target state. (repeated) |
| `ModifyLaunchTemplateDefaultVersion` | When `--default-version` is specified. | Perform the resource operation. |
| `DescribeLaunchTemplateVersions` | When `--default-version` is specified and `--no-wait` is not specified. | Poll until the resource reaches the target state. (repeated) |
| `DescribeLaunchTemplates` | When `&lt;id>` is specified and `--no-wait` is not specified. | Read the resource view. |
| `DescribeLaunchTemplates` | When `--name` is specified and `--no-wait` is not specified and `&lt;id>` is not specified. | Read the resource view. |
| `DescribeLaunchTemplateVersions` | When `--default-version` is specified and `--no-wait` is not specified. | Read the resource view. |
| `DescribeLaunchTemplateVersions` | When `--create-version` is specified and `--no-wait` is not specified. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--create-version` | boolean |  | create a new launch template version |
| `--default-version` | integer |  | launch template default version number |
| `--description` | string |  | instance description stored in the launch template |
| `--image` | string |  | ECS image ID |
| `--keypair` | string |  | key pair name |
| `--name` | string |  | launch template name |
| `--resource-resource-group` | string |  | resource group ID for resources created from the template |
| `--resource-tag` | key_value |  | tag assignment key=value for resources created from the template |
| `--security-groups` | array |  | security group IDs |
| `--sg` | string |  | security group ID |
| `--type` | string |  | ECS instance type |
| `--version-description` | string |  | launch template version description |
| `--vswitch` | string |  | vSwitch ID |

## delete

```bash
ecctl ecs launch-template delete <target> [flags]
```

Delete launch template or version

- Kind: `mutation` · Risk: high

| API | When called | Purpose |
|---|---|---|
| `DescribeLaunchTemplates` | When `&lt;target>` starts with `lt-`. | Read the resource view. |
| `DescribeLaunchTemplates` | When the preceding step did not produce `target_template`. | Read the resource view. |
| `DeleteLaunchTemplateVersion` | When `--version` is specified. | Perform the resource operation. |
| `DeleteLaunchTemplate` | When `--version` is not specified. | Perform the resource operation. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--version` | integer |  | launch template version number |

## get

```bash
ecctl ecs launch-template get <target> [flags]
```

Get launch template

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `DescribeLaunchTemplates` | When `&lt;target>` starts with `lt-`. | Read the resource view. |
| `DescribeLaunchTemplates` | When the preceding step did not produce `target_template`. | Read the resource view. |
| `DescribeLaunchTemplateVersions` | When `--with-versions` is specified. | Read the resource view. |
| `DescribeLaunchTemplateVersions` | When `--version` is specified and `--with-versions` is not specified. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | comma-separated resource fields to include |
| `--version` | integer |  | launch template version number |
| `--with-versions` | boolean |  | include launch template versions |

## list

```bash
ecctl ecs launch-template list [<targets>...] [flags]
```

List launch templates

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `DescribeLaunchTemplates` | When `&lt;targets>` is not specified. | Read the resource view. |
| `DescribeLaunchTemplates` | When `&lt;targets>` contains a value starting with `lt-`. | Read the resource view. |
| `DescribeLaunchTemplates` | When `&lt;targets>` contains a value that does not start with `lt-`. | Read the resource view. |
| `DescribeLaunchTemplates` | When `&lt;targets>` contains an unmatched value starting with `lt-`. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | comma-separated resource fields to include |
| `--filter` | key_value |  | filter expression key=value |
| `--limit` | integer |  | maximum resources to return (default: `100`) |
| `--page` | integer |  | results page to return (default: `1`) |

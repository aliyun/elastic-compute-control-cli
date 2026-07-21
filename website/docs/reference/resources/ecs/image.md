---
generated: true
generated_by: "website/scripts/gen-reference.mjs"
generated_command: "make build && npm --prefix website run gen:reference"
title: ecs image
sidebar_label: image
description: "Manage ECS image resources"
---

# ecs image

Manage ECS image resources

Run `ecctl ecs image <action> -h` for usage, or `ecctl schema ecs.image.<action> --full` for the complete, agent-readable spec of every parameter and behavior.

## create

```bash
ecctl ecs image create [flags]
```

Create custom image

- Kind: `mutation` · Risk: medium
- Synchronous: waits for `Available` (waiter `available_after_create`, timeout `1800s`); use `--no-wait` to skip.
- Idempotent via `ClientToken`.
- Dry run: supported via `--dry-run`.

| API | When called | Purpose |
|---|---|---|
| `CreateImage` | Every time the command runs. | Perform the resource operation. |
| `DescribeImages` | When `--no-wait` is not specified and `--dry-run` is not specified. | Poll until the resource reaches the target state. (repeated) |
| `DescribeImages` | When `--no-wait` is not specified and `--dry-run` is not specified. | Return the final resource view. (cached; no additional request) |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--architecture` | string |  | image architecture |
| `--boot-mode` | string |  | boot mode |
| `--description` | string |  | image description |
| `--detection-strategy` | string |  | image detection strategy |
| `--disk-device-mapping` | object |  | disk device mappings used when creating or importing an image |
| `--image-family` | string |  | image family to associate when creating or updating the image |
| `--image-version` | string |  | image version |
| `--instance` | string |  | instance ID used as the image source |
| `--name` | string |  | image name |
| `--platform` | string |  | operating system platform |
| `--resource-group` | string |  | resource group ID |
| `--snapshot` | string |  | snapshot ID used as the image source |
| `--tag` | key_value |  | tag assignment key=value |

## update

```bash
ecctl ecs image update <id> [flags]
```

Update image attributes or share permission

- Kind: `mutation` · Risk: medium

| API | When called | Purpose |
|---|---|---|
| `ModifyImageAttribute` | When `--name` is specified or `--description` is specified or `--image-family` is specified or `--boot-mode` is specified or `--license-type` is specified or `--status-action` is specified or `--features` is specified. | Perform the resource operation. |
| `ModifyImageSharePermission` | When `--share-add` is specified or `--share-remove` is specified or `--launch-permission` is specified. | Perform the resource operation. |
| `DescribeImages` | When `--no-wait` is not specified. | Read the resource view. |
| `DescribeImageSharePermission` | When (`--share-add` is specified or `--share-remove` is specified or `--launch-permission` is specified) and `--no-wait` is not specified. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--boot-mode` | string |  | boot mode |
| `--description` | string |  | image description |
| `--features` | object |  | image feature flags |
| `--image-family` | string |  | image family to associate when creating or updating the image |
| `--launch-permission` | string |  | image launch permission setting |
| `--license-type` | string |  | license type |
| `--name` | string |  | image name |
| `--share-add` | string_array |  | account IDs to add to the image share permission |
| `--share-remove` | string_array |  | account IDs to remove from the image share permission |
| `--status-action` | string |  | target status when modifying the image |

## delete

```bash
ecctl ecs image delete <id> [flags]
```

Delete image

- Kind: `mutation` · Risk: high
- Synchronous: waits for `absent` (waiter `deleted_after_delete`, timeout `600s`); use `--no-wait` to skip.

| API | When called | Purpose |
|---|---|---|
| `DeleteImage` | Every time the command runs. | Perform the resource operation. |
| `DescribeImages` | When `--no-wait` is not specified. | Poll until the resource reaches the target state. (repeated) |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--force` | boolean |  | force image deletion even when referenced by an instance (must be set explicitly) (default: `false`) |

## get

```bash
ecctl ecs image get <id> [flags]
```

Get image

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `DescribeImageFromFamily` | When `--family` is specified. | Read the resource view. |
| `DescribeImages` | When `&lt;id>` is specified. | Read the resource view. |
| `DescribeImageSharePermission` | When `--with-share-permission` is specified. | Read the resource view. |
| `DescribeImageSupportInstanceTypes` | When `--with-supported-instance-types` is specified. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--action-type` | string |  | action type for supported instance types query |
| `--family` | string |  | image family name |
| `--fields` | string |  | comma-separated resource fields to include |
| `--with-share-permission` | boolean |  | include image share permission |
| `--with-supported-instance-types` | boolean |  | include supported instance types |

## list

```bash
ecctl ecs image list [<ids>...] [flags]
```

List image resources

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `DescribeImages` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | comma-separated resource fields to include |
| `--filter` | key_value |  | filter expression key=value |
| `--limit` | integer |  | maximum resources to return (default: `100`) |
| `--page` | integer |  | results page to return (default: `1`) |

## copy

```bash
ecctl ecs image copy <id> [flags]
```

Copy image across regions

- Kind: `mutation` · Risk: medium
- Synchronous: waits for `Available` (waiter `available_after_copy`, timeout `3600s`); use `--no-wait` to skip.
- Idempotent via `ClientToken`.

| API | When called | Purpose |
|---|---|---|
| `CancelCopyImage` | When `--cancel` is specified. | Perform the resource operation. |
| `CopyImage` | When `--cancel` is not specified. | Perform the resource operation. |
| `DescribeImages` | When `--cancel` is not specified and `--no-wait` is not specified. | Poll until the resource reaches the target state. (repeated) |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--cancel` | boolean |  | cancel an in-progress copy task on the image |
| `--destination-description` | string |  | destination image description for copy |
| `--destination-name` | string |  | destination image name for copy |
| `--destination-region` | string |  | destination region for copy |
| `--encrypted` | boolean |  | encrypt the destination image when copying |
| `--kms-key-id` | string |  | KMS key ID used to encrypt the destination image |
| `--resource-group` | string |  | resource group ID |
| `--tag` | key_value |  | tag assignment key=value |

## export

```bash
ecctl ecs image export <id> [flags]
```

Export image to OSS

- Kind: `mutation` · Risk: medium
- Synchronous: waits for `Finished` (waiter `task_finished`, timeout `3600s`); use `--no-wait` to skip.

| API | When called | Purpose |
|---|---|---|
| `ExportImage` | Every time the command runs. | Perform the resource operation. |
| `DescribeTaskAttribute` | When `--no-wait` is not specified. | Poll until the resource reaches the target state. (repeated) |
| `DescribeTaskAttribute` | When `--no-wait` is not specified. | Return the final resource view. (cached; no additional request) |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--oss-bucket` | string | ✓ | OSS bucket for image export or import |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--export-format` | string |  | image export format |
| `--image-format` | string |  | image file format |
| `--oss-prefix` | string |  | OSS object key prefix for image export |
| `--role-name` | string |  | RAM role name for OSS access |

## import

```bash
ecctl ecs image import [flags]
```

Import image from OSS

- Kind: `mutation` · Risk: medium
- Synchronous: waits for `Available` (waiter `available_after_import`, timeout `3600s`); use `--no-wait` to skip.
- Idempotent via `ClientToken`.

| API | When called | Purpose |
|---|---|---|
| `ImportImage` | Every time the command runs. | Perform the resource operation. |
| `DescribeImages` | When `--no-wait` is not specified. | Poll until the resource reaches the target state. (repeated) |
| `DescribeImages` | When `--no-wait` is not specified. | Return the final resource view. (cached; no additional request) |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--disk-device-mapping` | object | ✓ | disk device mappings used when creating or importing an image |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--architecture` | string |  | image architecture |
| `--boot-mode` | string |  | boot mode |
| `--description` | string |  | image description |
| `--detection-strategy` | string |  | image detection strategy |
| `--image-family` | string |  | image family to associate when creating or updating the image |
| `--image-version` | string |  | image version |
| `--license-type` | string |  | license type |
| `--name` | string |  | image name |
| `--os-type` | string |  | operating system type |
| `--platform` | string |  | operating system platform |
| `--resource-group` | string |  | resource group ID |
| `--role-name` | string |  | RAM role name for OSS access |
| `--tag` | key_value |  | tag assignment key=value |

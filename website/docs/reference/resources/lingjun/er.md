---
generated: true
generated_by: "website/scripts/gen-reference.mjs"
generated_command: "make build && npm --prefix website run gen:reference"
title: lingjun er
sidebar_label: er
description: "Manage Lingjun Enterprise Router (HUB) resources"
---

# lingjun er

Manage Lingjun Enterprise Router (HUB) resources

Run `ecctl lingjun er <action> -h` for usage, or `ecctl schema lingjun.er.<action> --full` for the complete, agent-readable spec of every parameter and behavior.

## create

```bash
ecctl lingjun er create [flags]
```

Create Lingjun ER

- Kind: `mutation` · Risk: medium
- Synchronous: waits for `Available` (waiter `available_after_change`, timeout `600s`); use `--no-wait` to skip.

| API | When called | Purpose |
|---|---|---|
| `CreateEr` | Every time the command runs. | Perform the resource operation. |
| `GetEr` | When `--no-wait` is not specified. | Poll until the resource reaches the target state. (repeated) |
| `GetEr` | When `--no-wait` is not specified. | Return the final resource view. (cached; no additional request) |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--master-zone` | string | ✓ | master zone ID |
| `--name` | string | ✓ | ER name |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--description` | string |  | ER description |
| `--resource-group` | string |  | resource group ID |
| `--tag` | key_value |  | tag assignment key=value |

## update

```bash
ecctl lingjun er update <id> [flags]
```

Update Lingjun ER (attributes, attachments or route maps)

- Kind: `mutation` · Risk: medium
- Synchronous: waits for `Available` (waiter `available_after_change`, timeout `600s`); use `--no-wait` to skip.

| API | When called | Purpose |
|---|---|---|
| `UpdateEr` | When `--name` is specified or `--description` is specified. | Perform the resource operation. |
| `CreateErAttachment` | When `--attachment` contains a value prefixed with `+`. | Perform the resource operation. |
| `DeleteErAttachment` | When `--attachment` contains a value prefixed with `-`. | Perform the resource operation. |
| `CreateErRouteMap` | When `--route-map` contains a value prefixed with `+`. | Perform the resource operation. |
| `DeleteErRouteMap` | When `--route-map` contains a value prefixed with `-`. | Perform the resource operation. |
| `GetEr` | When `--no-wait` is not specified. | Poll until the resource reaches the target state. (repeated) |
| `GetEr` | When `--no-wait` is not specified. | Return the final resource view. (cached; no additional request) |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--attachment` | string |  | attachment change; prefix with + to add (inline kv: instance=...,instance_type=VPD\|VCC,name=...,auto_receive_all_route=true\|false[,resource_tenant=...]) or - to remove (value is the attachment ID, e.g. -era-xxx) |
| `--description` | string |  | ER description |
| `--name` | string |  | ER name |
| `--route-map` | string |  | route map change; prefix with + to add (inline kv: route_map_num=...,action=permit\|deny,reception_instance=...,reception_instance_type=VPD\|VCC,transmission_instance=...,transmission_instance_type=VPD\|VCC[,destination_cidr=...,description=...,reception_instance_owner=...,transmission_instance_owner=...]) or - to remove (value is the route-map ID, e.g. -ermap-xxx) |

## delete

```bash
ecctl lingjun er delete <id> [flags]
```

Delete Lingjun ER

- Kind: `mutation` · Risk: high
- Synchronous: waits for `absent` (waiter `deleted_after_delete`, timeout `600s`); use `--no-wait` to skip.

| API | When called | Purpose |
|---|---|---|
| `DeleteEr` | Every time the command runs. | Perform the resource operation. |
| `ListErs` | When `--no-wait` is not specified. | Poll until the resource reaches the target state. (repeated) |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |

## get

```bash
ecctl lingjun er get <id> [flags]
```

Get Lingjun ER

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `GetEr` | Every time the command runs. | Read the resource view. |
| `ListErAttachments` | When `--with-attachments` is specified. | Read the resource view. |
| `ListErRouteMaps` | When `--with-route-maps` is specified. | Read the resource view. |
| `ListErRouteEntries` | When `--with-routes` is specified. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | comma-separated resource fields to include |
| `--limit` | integer |  | maximum resources to return (default: `100`) |
| `--page` | integer |  | results page to return (default: `1`) |
| `--with-attachments` | boolean |  | include ER attachments list in get output |
| `--with-route-maps` | boolean |  | include ER route maps list in get output |
| `--with-routes` | boolean |  | include ER route entries list in get output |

## list

```bash
ecctl lingjun er list [flags]
```

List Lingjun ERs

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `ListErs` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | comma-separated resource fields to include |
| `--filter` | key_value |  | filter expression key=value |
| `--limit` | integer |  | maximum resources to return (default: `100`) |
| `--page` | integer |  | results page to return (default: `1`) |

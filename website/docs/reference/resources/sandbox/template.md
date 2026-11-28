---
generated: true
generated_by: "website/scripts/gen-reference.mjs"
generated_command: "make build && npm --prefix website run gen:reference"
title: sandbox template
sidebar_label: template
description: "Manage sandbox templates; use --openapi for native FC templates with Aliyun credentials."
---

# sandbox template

Manage sandbox templates; use --openapi for native FC templates with Aliyun credentials.

Run `ecctl sandbox template <action> -h` for usage, or `ecctl schema sandbox.template.<action> --full` for the complete, agent-readable spec of every parameter and behavior.

## create

```bash
ecctl sandbox template create [flags]
```

Create and build a sandbox template (E2B by default; native FC with --openapi)

- Kind: `mutation` · Risk: medium
- Synchronous: waits for `ready` (waiter `fc_ready`, timeout `15m`); use `--no-wait` to skip.

| API | When called | Purpose |
|---|---|---|
| `CreateTemplate` | When `--openapi` is specified. | Perform the resource operation. |
| `GetTemplate` | When `--openapi` is specified and `--no-wait` is not specified. | Poll until the resource reaches the target state. (repeated) |
| `CreateTemplate` | When `--openapi` is not specified. | Perform the resource operation. |
| `StartTemplateBuild` | When `--openapi` is not specified. | Perform the resource operation. |
| `GetTemplateBuildStatus` | When `--openapi` is not specified and `--no-wait` is not specified. | Poll until the resource reaches the target state. (repeated) |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--name` | string | ✓ | template name |
| `--build-config` | object |  | native FC build configuration as JSON or @file; --openapi update requires this or --runtime-config |
| `--cpu-count` | integer |  | template CPU count (default: `2`) |
| `--force` | boolean |  | bypass template build cache |
| `--from-image` | string |  | base container image |
| `--from-image-registry` | object |  | private registry credentials as JSON or @file |
| `--from-template` | string |  | base E2B template |
| `--memory-mb` | integer |  | template memory in MiB (default: `1024`) |
| `--openapi` | boolean |  | use native FC OpenAPI with Aliyun profile credentials and a region |
| `--ready-cmd` | string |  | template readiness command |
| `--runtime-config` | object |  | native FC runtime configuration as JSON or @file; nested API keys and JSON structure are preserved in output; --openapi update requires this or --build-config |
| `--start-cmd` | string |  | template start command |
| `--step` | object |  | E2B build steps as a JSON array or @file |
| `--tag` | string_array |  | template tags |
| `--team-id` | string |  | native FC team ID |

## update

```bash
ecctl sandbox template update <id> [flags]
```

Update template visibility or native FC configuration with --openapi

- Kind: `mutation` · Risk: medium
- Synchronous: waits for `ready` (waiter `fc_ready`, timeout `15m`); use `--no-wait` to skip.

| API | When called | Purpose |
|---|---|---|
| `UpdateTemplate` | When `--openapi` is specified. | Perform the resource operation. |
| `GetTemplate` | When `--openapi` is specified and `--no-wait` is not specified. | Poll until the resource reaches the target state. (repeated) |
| `UpdateTemplate` | When `--openapi` is not specified. | Perform the resource operation. |
| `GetTemplate` | When `--openapi` is not specified. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--build-config` | object |  | native FC build configuration as JSON or @file; --openapi update requires this or --runtime-config |
| `--openapi` | boolean |  | use native FC OpenAPI with Aliyun profile credentials and a region |
| `--public` | boolean |  | E2B visibility; E2B update requires --public, unavailable with --openapi |
| `--runtime-config` | object |  | native FC runtime configuration as JSON or @file; nested API keys and JSON structure are preserved in output; --openapi update requires this or --build-config |
| `--team-id` | string |  | native FC team ID |

## delete

```bash
ecctl sandbox template delete <id> [flags]
```

Delete a sandbox template (native FC with --openapi)

- Kind: `mutation` · Risk: high

| API | When called | Purpose |
|---|---|---|
| `DeleteTemplate` | When `--openapi` is specified. | Perform the resource operation. |
| `DeleteTemplate` | When `--openapi` is not specified. | Perform the resource operation. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--openapi` | boolean |  | use native FC OpenAPI with Aliyun profile credentials and a region |
| `--team-id` | string |  | native FC team ID |

## get

```bash
ecctl sandbox template get <id> [flags]
```

Get a sandbox template and its build state

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `GetTemplate` | When `--openapi` is specified. | Read the resource view. |
| `GetTemplate` | When `--openapi` is not specified. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--fields` | string |  | comma-separated resource fields to include |
| `--limit` | integer |  | maximum results to return (default: `100`) |
| `--next-token` | string |  | pagination token |
| `--openapi` | boolean |  | use native FC OpenAPI with Aliyun profile credentials and a region |
| `--team-id` | string |  | native FC team ID |

## list

```bash
ecctl sandbox template list [flags]
```

List sandbox templates (native FC with --openapi)

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `ListTemplates` | When `--openapi` is specified. | Read the resource view. |
| `ListTemplates` | When `--openapi` is not specified. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--fields` | string |  | comma-separated resource fields to include |
| `--limit` | integer |  | maximum results to return (default: `100`) |
| `--next-token` | string |  | pagination token |
| `--openapi` | boolean |  | use native FC OpenAPI with Aliyun profile credentials and a region |
| `--team-id` | string |  | native FC team ID |

## build-logs

```bash
ecctl sandbox template build-logs <id> [flags]
```

Read E2B template build logs

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `GetTemplateBuildLogs` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--build-id` | string | ✓ | template build ID |
| `--cursor` | integer |  | build log cursor in milliseconds |
| `--limit` | integer |  | maximum results to return (default: `100`) |

## build-status

```bash
ecctl sandbox template build-status <id> [flags]
```

Get an E2B template build status

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `GetTemplateBuildStatus` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--build-id` | string | ✓ | template build ID |

## publish

```bash
ecctl sandbox template publish <id> [flags]
```

Publish an E2B sandbox template

- Kind: `mutation` · Risk: medium

| API | When called | Purpose |
|---|---|---|
| `UpdateTemplate` | Every time the command runs. | Perform the resource operation. |
| `GetTemplate` | Every time the command runs. | Read the resource view. |

## tag-assign

```bash
ecctl sandbox template tag-assign [flags]
```

Assign tags to an E2B template build

- Kind: `mutation` · Risk: medium

| API | When called | Purpose |
|---|---|---|
| `AssignTemplateTags` | Every time the command runs. | Perform the resource operation. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--name` | string | ✓ | template name |
| `--tag` | string_array | ✓ | template tags |

## tag-delete

```bash
ecctl sandbox template tag-delete [flags]
```

Delete tags from E2B templates

- Kind: `mutation` · Risk: medium

| API | When called | Purpose |
|---|---|---|
| `DeleteTemplateTags` | Every time the command runs. | Perform the resource operation. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--name` | string | ✓ | template name |
| `--tag` | string_array | ✓ | template tags |

## tag-list

```bash
ecctl sandbox template tag-list <id> [flags]
```

List tags assigned to E2B template builds

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `ListTemplateTags` | Every time the command runs. | Read the resource view. |

## unpublish

```bash
ecctl sandbox template unpublish <id> [flags]
```

Make an E2B sandbox template private

- Kind: `mutation` · Risk: medium

| API | When called | Purpose |
|---|---|---|
| `UpdateTemplate` | Every time the command runs. | Perform the resource operation. |
| `GetTemplate` | Every time the command runs. | Read the resource view. |

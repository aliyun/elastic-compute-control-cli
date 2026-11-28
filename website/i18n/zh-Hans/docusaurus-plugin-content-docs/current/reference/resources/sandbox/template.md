---
generated: true
generated_by: "website/scripts/gen-reference.mjs"
generated_command: "make build && npm --prefix website run gen:reference"
title: sandbox template
sidebar_label: template
description: "管理沙箱模板；使用 --openapi 通过阿里云凭证管理原生 FC 模板。"
---

# sandbox template

管理沙箱模板；使用 --openapi 通过阿里云凭证管理原生 FC 模板。

运行 `ecctl sandbox template <action> -h` 查看用法，或 `ecctl schema sandbox.template.<action> --full` 获取该命令完整的结构化规格——每个参数与行为，便于 Agent 读取和填参。

## create

```bash
ecctl sandbox template create [flags]
```

创建并构建沙箱模板（默认 E2B；--openapi 使用原生 FC）

- 类型：`mutation` · 风险：medium
- 同步：等待 `ready`（waiter `fc_ready`，超时 `15m`）；用 `--no-wait` 跳过等待。

| API | 调用时机 | 用途 |
|---|---|---|
| `CreateTemplate` | 指定 `--openapi` 时 | 执行资源操作。 |
| `GetTemplate` | 指定 `--openapi` 且未指定 `--no-wait` 时 | 轮询等待资源达到目标状态。（重复调用） |
| `CreateTemplate` | 未指定 `--openapi` 时 | 执行资源操作。 |
| `StartTemplateBuild` | 未指定 `--openapi` 时 | 执行资源操作。 |
| `GetTemplateBuildStatus` | 未指定 `--openapi` 且未指定 `--no-wait` 时 | 轮询等待资源达到目标状态。（重复调用） |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--name` | string | ✓ | 模板名称 |
| `--build-config` | object |  | 原生 FC 构建配置，使用 JSON 或 @file；--openapi 更新需要此项或 --runtime-config |
| `--cpu-count` | integer |  | 模板 CPU 数量（默认：`2`） |
| `--force` | boolean |  | 跳过模板构建缓存 |
| `--from-image` | string |  | 基础容器镜像 |
| `--from-image-registry` | object |  | 私有镜像仓库凭证，使用 JSON 或 @file |
| `--from-template` | string |  | 基础 E2B 模板 |
| `--memory-mb` | integer |  | 模板内存（MiB）（默认：`1024`） |
| `--openapi` | boolean |  | 使用阿里云 profile 凭证和地域调用原生 FC OpenAPI |
| `--ready-cmd` | string |  | 模板就绪检查命令 |
| `--runtime-config` | object |  | 原生 FC 运行时配置，使用 JSON 或 @file；输出保留嵌套 API 键名和 JSON 结构；--openapi 更新需要此项或 --build-config |
| `--start-cmd` | string |  | 模板启动命令 |
| `--step` | object |  | E2B 构建步骤，使用 JSON 数组或 @file |
| `--tag` | string_array |  | 模板标签 |
| `--team-id` | string |  | 原生 FC 团队 ID |

## update

```bash
ecctl sandbox template update <id> [flags]
```

更新模板可见性，或通过 --openapi 更新原生 FC 配置

- 类型：`mutation` · 风险：medium
- 同步：等待 `ready`（waiter `fc_ready`，超时 `15m`）；用 `--no-wait` 跳过等待。

| API | 调用时机 | 用途 |
|---|---|---|
| `UpdateTemplate` | 指定 `--openapi` 时 | 执行资源操作。 |
| `GetTemplate` | 指定 `--openapi` 且未指定 `--no-wait` 时 | 轮询等待资源达到目标状态。（重复调用） |
| `UpdateTemplate` | 未指定 `--openapi` 时 | 执行资源操作。 |
| `GetTemplate` | 未指定 `--openapi` 时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--build-config` | object |  | 原生 FC 构建配置，使用 JSON 或 @file；--openapi 更新需要此项或 --runtime-config |
| `--openapi` | boolean |  | 使用阿里云 profile 凭证和地域调用原生 FC OpenAPI |
| `--public` | boolean |  | E2B 可见性；E2B 更新需要 --public，--openapi 不支持此项 |
| `--runtime-config` | object |  | 原生 FC 运行时配置，使用 JSON 或 @file；输出保留嵌套 API 键名和 JSON 结构；--openapi 更新需要此项或 --build-config |
| `--team-id` | string |  | 原生 FC 团队 ID |

## delete

```bash
ecctl sandbox template delete <id> [flags]
```

删除沙箱模板（--openapi 使用原生 FC）

- 类型：`mutation` · 风险：high

| API | 调用时机 | 用途 |
|---|---|---|
| `DeleteTemplate` | 指定 `--openapi` 时 | 执行资源操作。 |
| `DeleteTemplate` | 未指定 `--openapi` 时 | 执行资源操作。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--openapi` | boolean |  | 使用阿里云 profile 凭证和地域调用原生 FC OpenAPI |
| `--team-id` | string |  | 原生 FC 团队 ID |

## get

```bash
ecctl sandbox template get <id> [flags]
```

获取沙箱模板及其构建状态

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `GetTemplate` | 指定 `--openapi` 时 | 读取资源视图。 |
| `GetTemplate` | 未指定 `--openapi` 时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--fields` | string |  | 要包含的资源字段，使用逗号分隔 |
| `--limit` | integer |  | 最多返回结果数（默认：`100`） |
| `--next-token` | string |  | 分页令牌 |
| `--openapi` | boolean |  | 使用阿里云 profile 凭证和地域调用原生 FC OpenAPI |
| `--team-id` | string |  | 原生 FC 团队 ID |

## list

```bash
ecctl sandbox template list [flags]
```

列出沙箱模板（--openapi 使用原生 FC）

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `ListTemplates` | 指定 `--openapi` 时 | 读取资源视图。 |
| `ListTemplates` | 未指定 `--openapi` 时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--fields` | string |  | 要包含的资源字段，使用逗号分隔 |
| `--limit` | integer |  | 最多返回结果数（默认：`100`） |
| `--next-token` | string |  | 分页令牌 |
| `--openapi` | boolean |  | 使用阿里云 profile 凭证和地域调用原生 FC OpenAPI |
| `--team-id` | string |  | 原生 FC 团队 ID |

## build-logs

```bash
ecctl sandbox template build-logs <id> [flags]
```

读取 E2B 模板构建日志

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `GetTemplateBuildLogs` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--build-id` | string | ✓ | 模板构建 ID |
| `--cursor` | integer |  | 构建日志游标（毫秒） |
| `--limit` | integer |  | 最多返回结果数（默认：`100`） |

## build-status

```bash
ecctl sandbox template build-status <id> [flags]
```

获取 E2B 模板构建状态

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `GetTemplateBuildStatus` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--build-id` | string | ✓ | 模板构建 ID |

## publish

```bash
ecctl sandbox template publish <id> [flags]
```

公开 E2B 沙箱模板

- 类型：`mutation` · 风险：medium

| API | 调用时机 | 用途 |
|---|---|---|
| `UpdateTemplate` | 每次执行命令时 | 执行资源操作。 |
| `GetTemplate` | 每次执行命令时 | 读取资源视图。 |

## tag-assign

```bash
ecctl sandbox template tag-assign [flags]
```

为 E2B 模板构建分配标签

- 类型：`mutation` · 风险：medium

| API | 调用时机 | 用途 |
|---|---|---|
| `AssignTemplateTags` | 每次执行命令时 | 执行资源操作。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--name` | string | ✓ | 模板名称 |
| `--tag` | string_array | ✓ | 模板标签 |

## tag-delete

```bash
ecctl sandbox template tag-delete [flags]
```

删除 E2B 模板标签

- 类型：`mutation` · 风险：medium

| API | 调用时机 | 用途 |
|---|---|---|
| `DeleteTemplateTags` | 每次执行命令时 | 执行资源操作。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--name` | string | ✓ | 模板名称 |
| `--tag` | string_array | ✓ | 模板标签 |

## tag-list

```bash
ecctl sandbox template tag-list <id> [flags]
```

列出 E2B 模板构建标签

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `ListTemplateTags` | 每次执行命令时 | 读取资源视图。 |

## unpublish

```bash
ecctl sandbox template unpublish <id> [flags]
```

将 E2B 沙箱模板设为私有

- 类型：`mutation` · 风险：medium

| API | 调用时机 | 用途 |
|---|---|---|
| `UpdateTemplate` | 每次执行命令时 | 执行资源操作。 |
| `GetTemplate` | 每次执行命令时 | 读取资源视图。 |

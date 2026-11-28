---
generated: true
generated_by: "website/scripts/gen-reference.mjs"
generated_command: "make build && npm --prefix website run gen:reference"
title: sandbox volume
sidebar_label: volume
description: "管理原生 FC 沙箱存储卷配置。"
---

# sandbox volume

管理原生 FC 沙箱存储卷配置。

运行 `ecctl sandbox volume <action> -h` 查看用法，或 `ecctl schema sandbox.volume.<action> --full` 获取该命令完整的结构化规格——每个参数与行为，便于 Agent 读取和填参。

## create

```bash
ecctl sandbox volume create [flags]
```

创建原生 FC 沙箱存储卷

- 类型：`mutation` · 风险：medium
- 同步：等待 `AVAILABLE`（waiter `ready`，超时 `300s`）；用 `--no-wait` 跳过等待。

| API | 调用时机 | 用途 |
|---|---|---|
| `CreateVolume` | 每次执行命令时 | 执行资源操作。 |
| `GetVolume` | 未指定 `--no-wait` 时 | 轮询等待资源达到目标状态。（重复调用） |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--name` | string | ✓ | 团队内唯一的存储卷名称 |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--agentic-bucket-volume-config` | object |  | AgenticBucket 存储卷配置，使用 JSON 或 @file |
| `--agentic-fs-volume-config` | object |  | AgenticFS 存储卷配置，使用 JSON 或 @file |
| `--mount-config` | object |  | 挂载角色与 VPC 配置，使用 JSON 或 @file |
| `--oss-volume-config` | object |  | OSS 存储卷配置，使用 JSON 或 @file |
| `--team-id` | string |  | 所属团队 ID |

## update

```bash
ecctl sandbox volume update <id> [flags]
```

更新原生 FC 沙箱存储卷配置

- 类型：`mutation` · 风险：medium

| API | 调用时机 | 用途 |
|---|---|---|
| `UpdateVolume` | 每次执行命令时 | 执行资源操作。 |
| `GetVolume` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--team-id` | string | ✓ | 所属团队 ID |
| `--agentic-bucket-volume-config` | object |  | AgenticBucket 存储卷配置，使用 JSON 或 @file |
| `--agentic-fs-volume-config` | object |  | AgenticFS 存储卷配置，使用 JSON 或 @file |
| `--mount-config` | object |  | 挂载角色与 VPC 配置，使用 JSON 或 @file |
| `--oss-volume-config` | object |  | OSS 存储卷配置，使用 JSON 或 @file |

## delete

```bash
ecctl sandbox volume delete <id> [flags]
```

删除原生 FC 沙箱存储卷配置

- 类型：`mutation` · 风险：high
- 同步：等待 `absent`（waiter `absent_after_delete`，超时 `300s`）；用 `--no-wait` 跳过等待。

| API | 调用时机 | 用途 |
|---|---|---|
| `DeleteVolume` | 每次执行命令时 | 执行资源操作。 |
| `GetVolume` | 未指定 `--no-wait` 时 | 轮询等待资源达到目标状态。（重复调用） |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--team-id` | string |  | 所属团队 ID |

## get

```bash
ecctl sandbox volume get <id> [flags]
```

获取原生 FC 沙箱存储卷

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `GetVolume` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | 要包含的资源字段，使用逗号分隔 |
| `--team-id` | string |  | 所属团队 ID |

## list

```bash
ecctl sandbox volume list [flags]
```

列出原生 FC 沙箱存储卷

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `ListVolumes` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--all` | boolean |  | 从第一页获取所有令牌分页结果 |
| `--fields` | string |  | 要包含的资源字段，使用逗号分隔 |
| `--limit` | integer |  | 每页最多返回的存储卷数量（maxResults；CLI 上限 100）（默认：`100`） |
| `--name` | string |  | 团队内唯一的存储卷名称 |
| `--next-token` | string |  | 服务端分页令牌 |
| `--resource-group` | string |  | 资源组 ID |
| `--status` | string |  | 服务端存储卷状态 |
| `--team-id` | string |  | 所属团队 ID |
| `--user-id` | string |  | 创建者用户 ID |

---
generated: true
generated_by: "website/scripts/gen-reference.mjs"
generated_command: "make build && npm --prefix website run gen:reference"
title: rg service-linked-role
sidebar_label: service-linked-role
description: "管理服务关联角色"
---

# rg service-linked-role

管理服务关联角色

运行 `ecctl rg service-linked-role <action> -h` 查看用法，或 `ecctl schema rg.service-linked-role.<action> --full` 获取该命令完整的结构化规格——每个参数与行为，便于 Agent 读取和填参。

## create

```bash
ecctl rg service-linked-role create [flags]
```

创建服务关联角色

- 类型：`mutation` · 风险：medium

| API | 调用时机 | 用途 |
|---|---|---|
| `CreateServiceLinkedRole` | 每次执行命令时 | 执行资源操作。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--service-name` | string | ✓ | 云服务名称（例如 polardb.aliyuncs.com） |
| `--custom-suffix` | string |  | 角色名称自定义后缀 |
| `--description` | string |  | 角色描述 |

## delete

```bash
ecctl rg service-linked-role delete <name> [flags]
```

删除服务关联角色

- 类型：`mutation` · 风险：high
- 同步：等待 `SUCCEEDED`（waiter `deleted_after_delete`，超时 `300s`）；用 `--no-wait` 跳过等待。

| API | 调用时机 | 用途 |
|---|---|---|
| `DeleteServiceLinkedRole` | 每次执行命令时 | 执行资源操作。 |
| `GetServiceLinkedRoleDeletionStatus` | 每次执行命令时 | 轮询等待资源达到目标状态。（重复调用） |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |

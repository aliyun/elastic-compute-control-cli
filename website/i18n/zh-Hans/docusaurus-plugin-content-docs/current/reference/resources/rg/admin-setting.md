---
generated: true
generated_by: "website/scripts/gen-reference.mjs"
generated_command: "make build && npm --prefix website run gen:reference"
title: rg admin-setting
sidebar_label: admin-setting
description: "管理资源组管理员设置"
---

# rg admin-setting

管理资源组管理员设置

运行 `ecctl rg admin-setting <action> -h` 查看用法，或 `ecctl schema rg.admin-setting.<action> --full` 获取该命令完整的结构化规格——每个参数与行为，便于 Agent 读取和填参。

## update

```bash
ecctl rg admin-setting update [flags]
```

更新资源组管理员设置

- 类型：`mutation` · 风险：medium

| API | 调用时机 | 用途 |
|---|---|---|
| `UpdateResourceGroupAdminSetting` | 每次执行命令时 | 执行资源操作。 |
| `GetResourceGroupAdminSetting` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--creator-as-admin` | boolean | ✓ | 创建者是否为管理员 |
| `--region` | string | ✓ | Alibaba Cloud region |

## get

```bash
ecctl rg admin-setting get [flags]
```

获取资源组管理员设置

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `GetResourceGroupAdminSetting` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | 要包含的资源字段，使用逗号分隔 |

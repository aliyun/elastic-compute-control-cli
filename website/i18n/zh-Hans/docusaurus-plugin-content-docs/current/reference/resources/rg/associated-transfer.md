---
generated: true
generated_by: "website/scripts/gen-reference.mjs"
generated_command: "make build && npm --prefix website run gen:reference"
title: rg associated-transfer
sidebar_label: associated-transfer
description: "管理关联资源随转组设置"
---

# rg associated-transfer

管理关联资源随转组设置

运行 `ecctl rg associated-transfer <action> -h` 查看用法，或 `ecctl schema rg.associated-transfer.<action> --full` 获取该命令完整的结构化规格——每个参数与行为，便于 Agent 读取和填参。

## update

```bash
ecctl rg associated-transfer update [flags]
```

更新关联资源随转设置

- 类型：`mutation` · 风险：medium

| API | 调用时机 | 用途 |
|---|---|---|
| `UpdateAssociatedTransferSetting` | 每次执行命令时 | 执行资源操作。 |
| `ListAssociatedTransferSetting` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--enable-existing-resources-transfer` | string |  | 是否转移存量关联资源 |
| `--rule-setting` | object |  | 关联资源随转规则设置 |
| `--status` | string |  | 关联资源随转状态 |

## list

```bash
ecctl rg associated-transfer list [flags]
```

查看关联资源随转设置

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `ListAssociatedTransferSetting` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | 要包含的资源字段，使用逗号分隔 |

## disable

```bash
ecctl rg associated-transfer disable [flags]
```

禁用关联资源随转

- 类型：`mutation` · 风险：medium

| API | 调用时机 | 用途 |
|---|---|---|
| `DisableAssociatedTransfer` | 每次执行命令时 | 执行资源操作。 |
| `ListAssociatedTransferSetting` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |

## enable

```bash
ecctl rg associated-transfer enable [flags]
```

启用关联资源随转

- 类型：`mutation` · 风险：medium

| API | 调用时机 | 用途 |
|---|---|---|
| `EnableAssociatedTransfer` | 每次执行命令时 | 执行资源操作。 |
| `ListAssociatedTransferSetting` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |

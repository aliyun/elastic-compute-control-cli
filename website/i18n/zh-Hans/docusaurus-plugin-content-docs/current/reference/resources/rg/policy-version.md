---
generated: true
generated_by: "website/scripts/gen-reference.mjs"
generated_command: "make build && npm --prefix website run gen:reference"
title: rg policy version
sidebar_label: policy version
description: "管理权限策略版本"
---

# rg policy version

管理权限策略版本

运行 `ecctl rg policy version <action> -h` 查看用法，或 `ecctl schema rg.policy.version.<action> --full` 获取该命令完整的结构化规格——每个参数与行为，便于 Agent 读取和填参。

## create

```bash
ecctl rg policy version create [flags]
```

创建权限策略版本

- 类型：`mutation` · 风险：medium

| API | 调用时机 | 用途 |
|---|---|---|
| `CreatePolicyVersion` | 每次执行命令时 | 执行资源操作。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--policy-document` | string | ✓ | 权限策略内容 JSON 或 @file |
| `--policy-name` | string | ✓ | 权限策略名称 |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--set-as-default` | boolean |  | 设置为默认版本 |

## update

```bash
ecctl rg policy version update <version-id> [flags]
```

更新权限策略版本

- 类型：`mutation` · 风险：medium

| API | 调用时机 | 用途 |
|---|---|---|
| `SetDefaultPolicyVersion` | 指定 `--set-as-default` 时 | 执行资源操作。 |
| `GetPolicy` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--policy-name` | string | ✓ | 权限策略名称 |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--set-as-default` | boolean |  | 设置为默认版本 |

## delete

```bash
ecctl rg policy version delete <version-id> [flags]
```

删除权限策略版本

- 类型：`mutation` · 风险：high

| API | 调用时机 | 用途 |
|---|---|---|
| `SetDefaultPolicyVersion` | 指定 `--fallback-default-version` 时 | 执行资源操作。 |
| `DeletePolicyVersion` | 每次执行命令时 | 执行资源操作。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--policy-name` | string | ✓ | 权限策略名称 |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fallback-default-version` | string |  | 删除指定版本前恢复为该默认版本 |

## get

```bash
ecctl rg policy version get <version-id> [flags]
```

获取权限策略版本

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `GetPolicyVersion` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--policy-name` | string | ✓ | 权限策略名称 |
| `--policy-type` | string | ✓ | 权限策略类型 |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | 要包含的资源字段，使用逗号分隔 |

## list

```bash
ecctl rg policy version list [flags]
```

列出权限策略版本

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `ListPolicyVersions` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--policy-name` | string | ✓ | 权限策略名称 |
| `--policy-type` | string | ✓ | 权限策略类型 |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | 要包含的资源字段，使用逗号分隔 |

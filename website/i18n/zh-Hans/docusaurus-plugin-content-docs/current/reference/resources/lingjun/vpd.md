---
generated: true
generated_by: "website/scripts/gen-reference.mjs"
generated_command: "make build && npm --prefix website run gen:reference"
title: lingjun vpd
sidebar_label: vpd
description: "管理灵骏网段资源"
---

# lingjun vpd

管理灵骏网段资源

运行 `ecctl lingjun vpd <action> -h` 查看用法，或 `ecctl schema lingjun.vpd.<action> --full` 获取该命令完整的结构化规格——每个参数与行为，便于 Agent 读取和填参。

## create

```bash
ecctl lingjun vpd create [flags]
```

创建 VPD

- 类型：`mutation` · 风险：medium
- 同步：等待 `Available`（waiter `available_after_change`，超时 `300s`）；用 `--no-wait` 跳过等待。

| API | 调用时机 | 用途 |
|---|---|---|
| `CreateVpd` | 每次执行命令时 | 执行资源操作。 |
| `GetVpd` | 未指定 `--no-wait` 时 | 轮询等待资源达到目标状态。（重复调用） |
| `GetVpd` | 未指定 `--no-wait` 时 | 返回最终资源视图。（复用等待结果，不额外请求） |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--cidr` | cidr | ✓ | VPD 网段 |
| `--name` | string | ✓ | VPD 名称 |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--resource-group` | string |  | 资源组 ID |
| `--subnet` | object |  | 随 VPD 创建的子网 |
| `--tag` | key_value |  | 标签赋值 key=value |

## update

```bash
ecctl lingjun vpd update <id> [flags]
```

更新 VPD

- 类型：`mutation` · 风险：medium
- 同步：等待 `matched`（waiter `update_converged`，超时 `300s`）；用 `--no-wait` 跳过等待。

| API | 调用时机 | 用途 |
|---|---|---|
| `UpdateVpd` | 指定 `--name` 时 | 执行资源操作。 |
| `AssociateVpdCidrBlock` | `--cidr` 中包含以 `+` 为前缀的值时 | 执行资源操作。 |
| `UnAssociateVpdCidrBlock` | `--cidr` 中包含以 `-` 为前缀的值时 | 执行资源操作。 |
| `GetVpd` | 未指定 `--no-wait` 时 | 轮询等待资源达到目标状态。（重复调用） |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--cidr` | string |  | 辅助网段变更，使用 + 前缀关联或 - 前缀解除关联 |
| `--name` | string |  | VPD 名称 |

## delete

```bash
ecctl lingjun vpd delete <id> [flags]
```

删除 VPD

- 类型：`mutation` · 风险：high
- 同步：等待 `absent`（waiter `deleted_after_delete`，超时 `300s`）；用 `--no-wait` 跳过等待。

| API | 调用时机 | 用途 |
|---|---|---|
| `DeleteVpd` | 每次执行命令时 | 执行资源操作。 |
| `ListVpds` | 未指定 `--no-wait` 时 | 轮询等待资源达到目标状态。（重复调用） |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |

## get

```bash
ecctl lingjun vpd get <id> [flags]
```

获取 VPD

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `GetVpd` | 每次执行命令时 | 读取资源视图。 |
| `ListVpdRouteEntries` | 指定 `--with-routes` 时 | 读取资源视图。 |
| `ListVpdGrantRules` | 指定 `--with-grants` 时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | 要包含的资源字段，使用逗号分隔 |
| `--limit` | integer |  | 最多返回资源数量（默认：`100`） |
| `--page` | integer |  | 返回结果页码（默认：`1`） |
| `--with-grants` | boolean |  | 附带 VPD 授权规则 |
| `--with-routes` | boolean |  | 附带 VPD 路由条目 |

## list

```bash
ecctl lingjun vpd list [flags]
```

列出 VPD

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `ListVpds` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | 要包含的资源字段，使用逗号分隔 |
| `--filter` | key_value |  | 过滤表达式 key=value |
| `--limit` | integer |  | 最多返回资源数量（默认：`100`） |
| `--page` | integer |  | 返回结果页码（默认：`1`） |

---
generated: true
generated_by: "website/scripts/gen-reference.mjs"
generated_command: "make build && npm --prefix website run gen:reference"
title: lingjun subnet
sidebar_label: subnet
description: "管理灵骏子网资源"
---

# lingjun subnet

管理灵骏子网资源

运行 `ecctl lingjun subnet <action> -h` 查看用法，或 `ecctl schema lingjun.subnet.<action> --full` 获取该命令完整的结构化规格——每个参数与行为，便于 Agent 读取和填参。

## create

```bash
ecctl lingjun subnet create [flags]
```

创建灵骏子网

- 类型：`mutation` · 风险：medium
- 同步：等待 `Available`（waiter `available_after_change`，超时 `300s`）；用 `--no-wait` 跳过等待。

| API | 调用时机 | 用途 |
|---|---|---|
| `CreateSubnet` | 每次执行命令时 | 执行资源操作。 |
| `GetSubnet` | 未指定 `--no-wait` 时 | 轮询等待资源达到目标状态。（重复调用） |
| `GetSubnet` | 未指定 `--no-wait` 时 | 返回最终资源视图。（复用等待结果，不额外请求） |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--cidr` | cidr | ✓ | 子网 CIDR 网段 |
| `--name` | string | ✓ | 灵骏子网名称 |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--vpd` | string | ✓ | 灵骏 VPD ID |
| `--zone` | string | ✓ | 可用区 ID |
| `--tag` | key_value |  | 标签赋值 key=value |
| `--type` | string |  | 子网用途类型 |

## update

```bash
ecctl lingjun subnet update <id> [flags]
```

更新灵骏子网

- 类型：`mutation` · 风险：medium
- 同步：等待 `Available`（waiter `available_after_change`，超时 `300s`）；用 `--no-wait` 跳过等待。

| API | 调用时机 | 用途 |
|---|---|---|
| `UpdateSubnet` | 每次执行命令时 | 执行资源操作。 |
| `GetSubnet` | 未指定 `--no-wait` 时 | 轮询等待资源达到目标状态。（重复调用） |
| `GetSubnet` | 未指定 `--no-wait` 时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--vpd` | string | ✓ | 灵骏 VPD ID |
| `--zone` | string | ✓ | 可用区 ID |
| `--name` | string |  | 灵骏子网名称 |

## delete

```bash
ecctl lingjun subnet delete <id> [flags]
```

删除灵骏子网

- 类型：`mutation` · 风险：high
- 同步：等待 `absent`（waiter `deleted_after_delete`，超时 `300s`）；用 `--no-wait` 跳过等待。

| API | 调用时机 | 用途 |
|---|---|---|
| `DeleteSubnet` | 每次执行命令时 | 执行资源操作。 |
| `ListSubnets` | 未指定 `--no-wait` 时 | 轮询等待资源达到目标状态。（重复调用） |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--vpd` | string | ✓ | 灵骏 VPD ID |
| `--zone` | string | ✓ | 可用区 ID |

## get

```bash
ecctl lingjun subnet get <id> [flags]
```

获取灵骏子网

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `GetSubnet` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | 要包含的资源字段，使用逗号分隔 |
| `--vpd` | string |  | 灵骏 VPD ID |

## list

```bash
ecctl lingjun subnet list [flags]
```

列出灵骏子网

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `ListSubnets` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | 要包含的资源字段，使用逗号分隔 |
| `--filter` | key_value |  | 过滤表达式 key=value |
| `--limit` | integer |  | 最多返回资源数量（默认：`100`） |
| `--page` | integer |  | 返回结果页码（默认：`1`） |

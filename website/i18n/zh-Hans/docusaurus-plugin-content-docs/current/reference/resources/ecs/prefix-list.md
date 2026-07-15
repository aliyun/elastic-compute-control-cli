---
title: ecs prefix-list
sidebar_label: prefix-list
description: "管理前缀列表"
---

# ecs prefix-list

管理前缀列表

运行 `ecctl ecs prefix-list <action> -h` 查看用法，或 `ecctl schema ecs.prefix-list.<action> --full` 获取该命令完整的结构化规格——每个参数与行为，便于 Agent 读取和填参。

## create

```bash
ecctl ecs prefix-list create [flags]
```

创建前缀列表

- 类型：`mutation` · 风险：medium
- 同步：等待 `matched`（waiter `entries_visible`，超时 `300s`）；用 `--no-wait` 跳过等待。
- 通过 `ClientToken` 幂等。

| API | 调用时机 | 用途 |
|---|---|---|
| `CreatePrefixList` | 每次执行命令时 | 执行资源操作。 |
| `DescribePrefixListAttributes` | 未指定 `--no-wait` 且指定 `--entry` 时 | 轮询等待资源达到目标状态。（重复调用） |
| `DescribePrefixListAttributes` | 未指定 `--no-wait` 时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--address-family` | string | ✓ | IP 地址族 |
| `--max-entries` | integer | ✓ | 最大条目数量 |
| `--name` | string | ✓ | 前缀列表名称 |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--description` | string |  | 前缀列表描述 |
| `--entry` | object |  | 创建时的前缀列表条目 |
| `--resource-group` | string |  | 资源组 ID |
| `--tag` | key_value |  | 标签赋值 key=value |

## update

```bash
ecctl ecs prefix-list update <id> [flags]
```

更新前缀列表

- 类型：`mutation` · 风险：medium
- 同步：等待 `matched`（waiter `entries_visible`，超时 `300s`）；用 `--no-wait` 跳过等待。

| API | 调用时机 | 用途 |
|---|---|---|
| `ModifyPrefixList` | 每次执行命令时 | 执行资源操作。 |
| `DescribePrefixListAttributes` | 未指定 `--no-wait` 且 `--entry` 中包含以 `+` 为前缀的值时 | 轮询等待资源达到目标状态。（重复调用） |
| `DescribePrefixListAttributes` | 未指定 `--no-wait` 且 `--entry` 中包含以 `-` 为前缀的值时 | 轮询等待资源达到目标状态。（重复调用） |
| `DescribePrefixListAttributes` | 未指定 `--no-wait` 时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--description` | string |  | 前缀列表描述 |
| `--entry` | string |  | 条目变更，使用 + 前缀新增或 - 前缀删除 |
| `--name` | string |  | 前缀列表名称 |

## delete

```bash
ecctl ecs prefix-list delete <id> [flags]
```

删除前缀列表

- 类型：`mutation` · 风险：high

| API | 调用时机 | 用途 |
|---|---|---|
| `DeletePrefixList` | 每次执行命令时 | 执行资源操作。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |

## get

```bash
ecctl ecs prefix-list get <id> [flags]
```

获取前缀列表

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `DescribePrefixListAttributes` | 每次执行命令时 | 读取资源视图。 |
| `DescribePrefixListAssociations` | 指定 `--with-associations` 时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | 要包含的资源字段，使用逗号分隔 |
| `--with-associations` | boolean |  | 附带关联资源 |

## list

```bash
ecctl ecs prefix-list list [<ids>...] [flags]
```

列出前缀列表

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `DescribePrefixLists` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | 要包含的资源字段，使用逗号分隔 |
| `--filter` | key_value |  | 过滤表达式 key=value |
| `--limit` | integer |  | 最多返回资源数量（默认：`100`） |
| `--next-token` | string |  | 下一页查询 token |

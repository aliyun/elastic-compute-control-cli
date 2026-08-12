---
generated: true
generated_by: "website/scripts/gen-reference.mjs"
generated_command: "make build && npm --prefix website run gen:reference"
title: lingjun er
sidebar_label: er
description: "管理灵骏 HUB（Enterprise Router）资源"
---

# lingjun er

管理灵骏 HUB（Enterprise Router）资源

运行 `ecctl lingjun er <action> -h` 查看用法，或 `ecctl schema lingjun.er.<action> --full` 获取该命令完整的结构化规格——每个参数与行为，便于 Agent 读取和填参。

## create

```bash
ecctl lingjun er create [flags]
```

创建灵骏 HUB

- 类型：`mutation` · 风险：medium
- 同步：等待 `Available`（waiter `available_after_change`，超时 `600s`）；用 `--no-wait` 跳过等待。

| API | 调用时机 | 用途 |
|---|---|---|
| `CreateEr` | 每次执行命令时 | 执行资源操作。 |
| `GetEr` | 未指定 `--no-wait` 时 | 轮询等待资源达到目标状态。（重复调用） |
| `GetEr` | 未指定 `--no-wait` 时 | 返回最终资源视图。（复用等待结果，不额外请求） |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--master-zone` | string | ✓ | 主可用区 ID |
| `--name` | string | ✓ | HUB 名称 |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--description` | string |  | HUB 描述 |
| `--resource-group` | string |  | 资源组 ID |
| `--tag` | key_value |  | 标签赋值 key=value |

## update

```bash
ecctl lingjun er update <id> [flags]
```

更新灵骏 HUB（基础属性、连接关系或路由策略）

- 类型：`mutation` · 风险：medium
- 同步：等待 `Available`（waiter `available_after_change`，超时 `600s`）；用 `--no-wait` 跳过等待。

| API | 调用时机 | 用途 |
|---|---|---|
| `UpdateEr` | 指定 `--name` 或指定 `--description` 时 | 执行资源操作。 |
| `CreateErAttachment` | `--attachment` 中包含以 `+` 为前缀的值时 | 执行资源操作。 |
| `DeleteErAttachment` | `--attachment` 中包含以 `-` 为前缀的值时 | 执行资源操作。 |
| `CreateErRouteMap` | `--route-map` 中包含以 `+` 为前缀的值时 | 执行资源操作。 |
| `DeleteErRouteMap` | `--route-map` 中包含以 `-` 为前缀的值时 | 执行资源操作。 |
| `GetEr` | 未指定 `--no-wait` 时 | 轮询等待资源达到目标状态。（重复调用） |
| `GetEr` | 未指定 `--no-wait` 时 | 返回最终资源视图。（复用等待结果，不额外请求） |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--attachment` | string |  | 连接关系变更；+ 前缀新增（内联 kv：instance=...,instance_type=VPD\|VCC,name=...,auto_receive_all_route=true\|false[,resource_tenant=...]），- 前缀按 attachment ID 删除（如 -era-xxx） |
| `--description` | string |  | HUB 描述 |
| `--name` | string |  | HUB 名称 |
| `--route-map` | string |  | 路由策略变更；+ 前缀新增（内联 kv：route_map_num=...,action=permit\|deny,reception_instance=...,reception_instance_type=VPD\|VCC,transmission_instance=...,transmission_instance_type=VPD\|VCC[,destination_cidr=...,description=...,reception_instance_owner=...,transmission_instance_owner=...]），- 前缀按 route-map ID 删除（如 -ermap-xxx） |

## delete

```bash
ecctl lingjun er delete <id> [flags]
```

删除灵骏 HUB

- 类型：`mutation` · 风险：high
- 同步：等待 `absent`（waiter `deleted_after_delete`，超时 `600s`）；用 `--no-wait` 跳过等待。

| API | 调用时机 | 用途 |
|---|---|---|
| `DeleteEr` | 每次执行命令时 | 执行资源操作。 |
| `ListErs` | 未指定 `--no-wait` 时 | 轮询等待资源达到目标状态。（重复调用） |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |

## get

```bash
ecctl lingjun er get <id> [flags]
```

获取灵骏 HUB

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `GetEr` | 每次执行命令时 | 读取资源视图。 |
| `ListErAttachments` | 指定 `--with-attachments` 时 | 读取资源视图。 |
| `ListErRouteMaps` | 指定 `--with-route-maps` 时 | 读取资源视图。 |
| `ListErRouteEntries` | 指定 `--with-routes` 时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | 要包含的资源字段，使用逗号分隔 |
| `--limit` | integer |  | 最多返回资源数量（默认：`100`） |
| `--page` | integer |  | 返回结果页码（默认：`1`） |
| `--with-attachments` | boolean |  | get 输出附带 HUB 连接关系列表 |
| `--with-route-maps` | boolean |  | get 输出附带 HUB 路由策略列表 |
| `--with-routes` | boolean |  | get 输出附带 HUB 路由条目列表 |

## list

```bash
ecctl lingjun er list [flags]
```

列出灵骏 HUB

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `ListErs` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | 要包含的资源字段，使用逗号分隔 |
| `--filter` | key_value |  | 过滤表达式 key=value |
| `--limit` | integer |  | 最多返回资源数量（默认：`100`） |
| `--page` | integer |  | 返回结果页码（默认：`1`） |

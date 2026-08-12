---
generated: true
generated_by: "website/scripts/gen-reference.mjs"
generated_command: "make build && npm --prefix website run gen:reference"
title: lingjun eni
sidebar_label: eni
description: "管理灵骏弹性网卡"
---

# lingjun eni

管理灵骏弹性网卡

运行 `ecctl lingjun eni <action> -h` 查看用法，或 `ecctl schema lingjun.eni.<action> --full` 获取该命令完整的结构化规格——每个参数与行为，便于 Agent 读取和填参。

## create

```bash
ecctl lingjun eni create [flags]
```

创建灵骏弹性网卡

- 类型：`mutation` · 风险：medium
- 同步：等待 `Unattached`（waiter `unattached_after_create`，超时 `300s`）；用 `--no-wait` 跳过等待。
- 通过 `ClientToken` 幂等。

| API | 调用时机 | 用途 |
|---|---|---|
| `CreateElasticNetworkInterface` | 每次执行命令时 | 执行资源操作。 |
| `GetElasticNetworkInterface` | 未指定 `--no-wait` 时 | 轮询等待资源达到目标状态。（重复调用） |
| `GetElasticNetworkInterface` | 未指定 `--no-wait` 时 | 返回最终资源视图。（复用等待结果，不额外请求） |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--subnet` | string | ✓ | 云上交换机 ID |
| `--vpd` | string | ✓ | 云上 VPC ID |
| `--zone` | string | ✓ | 可用区 ID |
| `--description` | string |  | 弹性网卡描述 |
| `--enable-jumbo-frame` | boolean |  | 启用巨帧 |
| `--node` | string |  | 灵骏节点 ID |
| `--resource-group` | string |  | 资源组 ID |
| `--security-group` | string |  | 安全组 ID |
| `--tag` | key_value |  | 标签赋值 key=value |

## update

```bash
ecctl lingjun eni update <id> [flags]
```

更新灵骏弹性网卡

- 类型：`mutation` · 风险：medium
- 通过 `ClientToken` 幂等。

| API | 调用时机 | 用途 |
|---|---|---|
| `UpdateElasticNetworkInterface` | 指定 `--description` 或指定 `--security-group` 时 | 执行资源操作。 |
| `AssignLeniPrivateIpAddress` | `--ip` 中包含以 `+` 为前缀的值时 | 执行资源操作。 |
| `UnassignLeniPrivateIpAddress` | `--ip` 中包含以 `-` 为前缀的值时 | 执行资源操作。 |
| `GetElasticNetworkInterface` | 未指定 `--no-wait` 时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--description` | string |  | 弹性网卡描述 |
| `--ip` | string |  | IP 变更，使用 + 前缀分配或 - 前缀释放 |
| `--security-group` | string |  | 安全组 ID |

## delete

```bash
ecctl lingjun eni delete <id> [flags]
```

删除灵骏弹性网卡

- 类型：`mutation` · 风险：high
- 同步：等待 `absent`（waiter `deleted_after_delete`，超时 `300s`）；用 `--no-wait` 跳过等待。

| API | 调用时机 | 用途 |
|---|---|---|
| `DeleteElasticNetworkInterface` | 每次执行命令时 | 执行资源操作。 |
| `ListElasticNetworkInterfaces` | 未指定 `--no-wait` 时 | 轮询等待资源达到目标状态。（重复调用） |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |

## get

```bash
ecctl lingjun eni get <id> [flags]
```

获取灵骏弹性网卡

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `GetElasticNetworkInterface` | 每次执行命令时 | 读取资源视图。 |
| `ListLeniPrivateIpAddresses` | 指定 `--with-ips` 时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | 要包含的资源字段，使用逗号分隔 |
| `--with-ips` | boolean |  | 附带查询辅助私网 IP 地址 |

## list

```bash
ecctl lingjun eni list [flags]
```

列出灵骏弹性网卡

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `ListElasticNetworkInterfaces` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | 要包含的资源字段，使用逗号分隔 |
| `--filter` | key_value |  | 过滤表达式 key=value |
| `--limit` | integer |  | 最多返回资源数量（默认：`100`） |
| `--page` | integer |  | 返回结果页码（默认：`1`） |

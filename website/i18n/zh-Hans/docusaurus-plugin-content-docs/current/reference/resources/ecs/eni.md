---
title: ecs eni
sidebar_label: eni
description: "管理弹性网卡"
---

# ecs eni

管理弹性网卡

运行 `ecctl ecs eni <action> -h` 查看用法，或 `ecctl schema ecs.eni.<action> --full` 获取该命令完整的结构化规格——每个参数与行为，便于 Agent 读取和填参。

## create

```bash
ecctl ecs eni create [flags]
```

创建弹性网卡

- 类型：`mutation` · 风险：medium
- 同步：等待 `Available`（waiter `available_after_create`，超时 `300s`）；用 `--no-wait` 跳过等待。
- 通过 `ClientToken` 幂等。

| API | 调用时机 | 用途 |
|---|---|---|
| `CreateNetworkInterface` | 每次执行命令时 | 执行资源操作。 |
| `DescribeNetworkInterfaceAttribute` | 未指定 `--no-wait` 时 | 轮询等待资源达到目标状态。（重复调用） |
| `DescribeNetworkInterfaceAttribute` | 未指定 `--no-wait` 时 | 返回最终资源视图。（复用等待结果，不额外请求） |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--sg` | string_array | ✓ | 安全组 ID 列表 |
| `--vswitch` | string | ✓ | 交换机 ID |
| `--connection-tracking` | object |  | 连接跟踪配置 JSON 对象 |
| `--delete-on-release` | boolean |  | 释放实例时删除网卡 |
| `--description` | string |  | 弹性网卡描述 |
| `--enhanced-network` | object |  | 增强网络配置 JSON 对象 |
| `--ipv4-prefix-count` | integer |  | IPv4 前缀数量 |
| `--ipv4-prefixes` | string_array |  | IPv4 前缀列表 |
| `--ipv6-address-count` | integer |  | IPv6 地址数量 |
| `--ipv6-addresses` | string_array |  | IPv6 地址列表 |
| `--ipv6-prefix-count` | integer |  | IPv6 前缀数量 |
| `--ipv6-prefixes` | string_array |  | IPv6 前缀列表 |
| `--name` | string |  | 弹性网卡名称 |
| `--primary-ip` | string |  | 主私网 IPv4 地址 |
| `--private-ip-count` | integer |  | 辅助私网 IPv4 地址数量 |
| `--private-ips` | string_array |  | 辅助私网 IPv4 地址列表 |
| `--queue-number` | integer |  | 网卡队列数 |
| `--queue-pair-number` | integer |  | 网卡队列对数量 |
| `--resource-group` | string |  | 资源组 ID |
| `--rx-queue-size` | integer |  | 接收队列深度 |
| `--source-dest-check` | boolean |  | 启用源/目标检查 |
| `--tag` | key_value |  | 标签赋值 key=value |
| `--traffic-config` | object |  | 网卡流量配置 |
| `--traffic-mode` | string |  | 网卡通讯模式 |
| `--tx-queue-size` | integer |  | 发送队列深度 |
| `--type` | string |  | 弹性网卡类型 |
| `--visible` | boolean |  | 网卡是否可见 |

## update

```bash
ecctl ecs eni update <id> [flags]
```

更新弹性网卡

- 类型：`mutation` · 风险：medium
- 通过 `ClientToken` 幂等。

| API | 调用时机 | 用途 |
|---|---|---|
| `ModifyNetworkInterfaceAttribute` | 指定 `--name` 或指定 `--description` 或指定 `--sg` 或指定 `--queue-number` 或指定 `--delete-on-release` 或指定 `--rx-queue-size` 或指定 `--tx-queue-size` 或指定 `--source-dest-check` 或指定 `--traffic-config` 或指定 `--connection-tracking` 或指定 `--enhanced-network` 时 | 执行资源操作。 |
| `AssignPrivateIpAddresses` | `--private-ip` 中包含以 `+` 为前缀的值或指定 `--private-ip-count` 或 `--ipv4-prefix` 中包含以 `+` 为前缀的值或指定 `--ipv4-prefix-count` 时 | 执行资源操作。 |
| `UnassignPrivateIpAddresses` | `--private-ip` 中包含以 `-` 为前缀的值或 `--ipv4-prefix` 中包含以 `-` 为前缀的值时 | 执行资源操作。 |
| `AssignIpv6Addresses` | `--ipv6-address` 中包含以 `+` 为前缀的值或指定 `--ipv6-address-count` 或 `--ipv6-prefix` 中包含以 `+` 为前缀的值或指定 `--ipv6-prefix-count` 时 | 执行资源操作。 |
| `UnassignIpv6Addresses` | `--ipv6-address` 中包含以 `-` 为前缀的值或 `--ipv6-prefix` 中包含以 `-` 为前缀的值时 | 执行资源操作。 |
| `EnableNetworkInterfaceQoS` | `--qos` 的 `status` 字段等于 `enable` 时 | 执行资源操作。 |
| `DisableNetworkInterfaceQoS` | `--qos` 的 `status` 字段等于 `disable` 时 | 执行资源操作。 |
| `DescribeNetworkInterfaceAttribute` | 未指定 `--no-wait` 时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--connection-tracking` | object |  | 连接跟踪配置 JSON 对象 |
| `--delete-on-release` | boolean |  | 释放实例时删除网卡 |
| `--description` | string |  | 弹性网卡描述 |
| `--enhanced-network` | object |  | 增强网络配置 JSON 对象 |
| `--ipv4-prefix` | string_array |  | IPv4 前缀变更 |
| `--ipv4-prefix-count` | integer |  | IPv4 前缀数量 |
| `--ipv6-address` | string_array |  | IPv6 地址变更 |
| `--ipv6-address-count` | integer |  | IPv6 地址数量 |
| `--ipv6-prefix` | string_array |  | IPv6 前缀变更 |
| `--ipv6-prefix-count` | integer |  | IPv6 前缀数量 |
| `--name` | string |  | 弹性网卡名称 |
| `--private-ip` | string_array |  | 辅助私网 IPv4 地址变更 |
| `--private-ip-count` | integer |  | 辅助私网 IPv4 地址数量 |
| `--qos` | object |  | 弹性网卡 QoS 限速配置 |
| `--queue-number` | integer |  | 网卡队列数 |
| `--rx-queue-size` | integer |  | 接收队列深度 |
| `--sg` | string_array |  | 安全组 ID 列表 |
| `--source-dest-check` | boolean |  | 启用源/目标检查 |
| `--traffic-config` | object |  | 网卡流量配置 |
| `--tx-queue-size` | integer |  | 发送队列深度 |

## delete

```bash
ecctl ecs eni delete <id> [flags]
```

删除弹性网卡

- 类型：`mutation` · 风险：high
- 同步：等待 `absent`（waiter `deleted_after_delete`，超时 `300s`）；用 `--no-wait` 跳过等待。

| API | 调用时机 | 用途 |
|---|---|---|
| `DeleteNetworkInterface` | 每次执行命令时 | 执行资源操作。 |
| `DescribeNetworkInterfaces` | 未指定 `--no-wait` 时 | 轮询等待资源达到目标状态。（重复调用） |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |

## get

```bash
ecctl ecs eni get <id> [flags]
```

获取弹性网卡

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `DescribeNetworkInterfaceAttribute` | 每次执行命令时 | 读取资源视图。 |
| `DescribeEniMonitorData` | 指定 `--with-monitor` 时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--attribute` | string |  | 要查询的弹性网卡属性 |
| `--end-time` | string |  | 监控结束时间 |
| `--fields` | string |  | 要包含的资源字段，使用逗号分隔 |
| `--instance` | string |  | ECS 实例 ID |
| `--period` | integer |  | 监控周期，单位秒 |
| `--start-time` | string |  | 监控开始时间 |
| `--with-monitor` | boolean |  | 附带查询网卡监控数据 |

## list

```bash
ecctl ecs eni list [<ids>...] [flags]
```

列出弹性网卡

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `DescribeNetworkInterfaces` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | 要包含的资源字段，使用逗号分隔 |
| `--filter` | key_value |  | 过滤表达式 key=value |
| `--limit` | integer |  | 最多返回资源数量（默认：`100`） |
| `--next-token` | string |  | 下一页查询 token |

## attach

```bash
ecctl ecs eni attach <id> [flags]
```

附加弹性网卡

- 类型：`mutation` · 风险：medium
- 同步：等待 `InUse`（waiter `in_use_after_attach`，超时 `300s`）；用 `--no-wait` 跳过等待。

| API | 调用时机 | 用途 |
|---|---|---|
| `AttachNetworkInterface` | 每次执行命令时 | 执行资源操作。 |
| `DescribeNetworkInterfaceAttribute` | 未指定 `--no-wait` 时 | 轮询等待资源达到目标状态。（重复调用） |
| `DescribeNetworkInterfaceAttribute` | 未指定 `--no-wait` 时 | 返回最终资源视图。（复用等待结果，不额外请求） |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--instance` | string | ✓ | ECS 实例 ID |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--network-card-index` | integer |  | 网卡索引 |
| `--trunk-network-instance-id` | string |  | Trunk ENI 实例 ID |
| `--wait-for-network-configuration-ready` | boolean |  | 附加时等待网络配置就绪 |

## detach

```bash
ecctl ecs eni detach <id> [flags]
```

分离弹性网卡

- 类型：`mutation` · 风险：medium
- 同步：等待 `Available`（waiter `available_after_detach`，超时 `300s`）；用 `--no-wait` 跳过等待。

| API | 调用时机 | 用途 |
|---|---|---|
| `DetachNetworkInterface` | 每次执行命令时 | 执行资源操作。 |
| `DescribeNetworkInterfaceAttribute` | 未指定 `--no-wait` 时 | 轮询等待资源达到目标状态。（重复调用） |
| `DescribeNetworkInterfaceAttribute` | 未指定 `--no-wait` 时 | 返回最终资源视图。（复用等待结果，不额外请求） |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--instance` | string | ✓ | ECS 实例 ID |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--trunk-network-instance-id` | string |  | Trunk ENI 实例 ID |

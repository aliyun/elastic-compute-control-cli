---
generated: true
generated_by: "website/scripts/gen-reference.mjs"
generated_command: "make build && npm --prefix website run gen:reference"
title: ecs instance
sidebar_label: instance
description: "管理实例资源"
---

# ecs instance

管理实例资源

运行 `ecctl ecs instance <action> -h` 查看用法，或 `ecctl schema ecs.instance.<action> --full` 获取该命令完整的结构化规格——每个参数与行为，便于 Agent 读取和填参。

## create

```bash
ecctl ecs instance create [flags]
```

创建实例

- 类型：`mutation` · 风险：medium
- 同步：等待 `Running`（waiter `running_after_create`，超时 `300s`）；用 `--no-wait` 跳过等待。
- 通过 `ClientToken` 幂等。
- 支持 `--dry-run` 校验。

| API | 调用时机 | 用途 |
|---|---|---|
| `DescribeImages` | 当 `--image` 非空且不以 `.vhd` 结尾时 | 创建实例前，将指定的镜像名称解析为镜像 ID。 |
| `RunInstances` | 每次执行命令时 | 执行资源操作。 |
| `DescribeInstances` | 未指定 `--no-wait` 且未指定 `--dry-run` 时 | 轮询等待资源达到目标状态。（重复调用） |
| `DescribeInstances` | 未指定 `--no-wait` 且未指定 `--dry-run` 时 | 返回最终资源视图。（复用等待结果，不额外请求） |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--image` | string | ✓ | ECS 镜像 ID 或名称 |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--sg` | string | ✓ | 安全组 ID |
| `--type` | string | ✓ | 实例规格 |
| `--vswitch` | string | ✓ | 交换机 ID |
| `--affinity` | string |  | 实例关联策略 |
| `--amount` | integer |  | 创建实例数量 |
| `--arn` | object |  | 实例使用的 RAM 角色 ARN 列表。 |
| `--auto-pay` | boolean |  | 自动支付预付费实例 |
| `--auto-release-time` | string |  | 自动释放时间 |
| `--auto-renew` | boolean |  | 自动续费预付费实例 |
| `--auto-renew-period` | integer |  | 自动续费周期 |
| `--clock-options` | object |  | 时钟选项 |
| `--cpu-options` | object |  | CPU 选项 |
| `--credit-specification` | string |  | 突发性能实例积分模式 |
| `--data-disk` | object |  | 创建实例时挂载的数据盘列表。 |
| `--dedicated-host` | string |  | 专有宿主机 ID |
| `--deletion-protection` | boolean |  | 启用删除保护 |
| `--deployment-set` | string |  | 部署集 ID |
| `--deployment-set-group-no` | integer |  | 部署集分组编号 |
| `--description` | string |  | 实例描述 |
| `--hibernation-options` | object |  | 休眠选项 |
| `--host-name` | string |  | 主机名 |
| `--host-names` | array |  | 主机名列表 |
| `--hpc-cluster` | string |  | HPC 集群 ID |
| `--http-endpoint` | string |  | 元数据服务访问配置 |
| `--http-put-response-hop-limit` | integer |  | 元数据服务 PUT 响应跳数限制 |
| `--http-tokens` | string |  | 元数据服务 Token 要求 |
| `--image-family` | string |  | 镜像族系 |
| `--image-options` | object |  | 镜像选项 |
| `--instance-charge-type` | string |  | 实例付费类型（默认：`PostPaid`） |
| `--internet-bandwidth-in` | integer |  | 公网入带宽 |
| `--internet-bandwidth-out` | integer |  | 公网出带宽 |
| `--internet-charge-type` | string |  | 公网计费类型 |
| `--io-optimized` | string |  | I/O 优化配置 |
| `--ipv6-address-count` | integer |  | IPv6 地址数量 |
| `--ipv6-addresses` | array |  | IPv6 地址列表 |
| `--isp` | string |  | 线路运营商 |
| `--key-pair` | string |  | 密钥对名称 |
| `--launch-template` | string |  | 启动模板 ID |
| `--launch-template-name` | string |  | 启动模板名称 |
| `--launch-template-version` | integer |  | 启动模板版本 |
| `--min-amount` | integer |  | 最小创建实例数量 |
| `--name` | string |  | 实例名称 |
| `--network-interface` | object |  | 弹性网卡列表。 |
| `--network-interface-queue-number` | integer |  | 弹性网卡队列数 |
| `--network-options` | object |  | 网络选项 |
| `--password` | string |  | 实例密码 |
| `--password-inherit` | boolean |  | 继承镜像密码 |
| `--period` | integer |  | 预付费周期 |
| `--period-unit` | string |  | 预付费周期单位 |
| `--private-dns-name-options` | object |  | 私有 DNS 名称选项 |
| `--private-ip` | string |  | 私网 IPv4 地址 |
| `--private-pool-options` | object |  | 私有池选项 |
| `--ram-role` | string |  | RAM 角色名称 |
| `--resource-group` | string |  | 资源组 ID |
| `--scheduler-options` | object |  | 调度选项 |
| `--security-enhancement-strategy` | string |  | 安全增强策略 |
| `--security-group-ids` | array |  | 安全组 ID 列表 |
| `--security-options` | object |  | 安全选项 |
| `--spot-duration` | integer |  | 抢占式实例持续时间 |
| `--spot-interruption-behavior` | string |  | 抢占中断行为 |
| `--spot-price-limit` | number |  | 抢占式实例价格上限 |
| `--spot-strategy` | string |  | 抢占式实例策略 |
| `--storage-set` | string |  | 存储集 ID |
| `--storage-set-partition-number` | integer |  | 存储集分区编号 |
| `--system-disk` | object |  | 系统盘配置。 |
| `--tag` | key_value |  | 标签赋值 key=value |
| `--tenancy` | string |  | 租户属性 |
| `--unique-suffix` | boolean |  | 为生成名称添加唯一后缀 |
| `--user-data` | string |  | 用户数据 |
| `--zone` | string |  | 可用区 ID |

## update

```bash
ecctl ecs instance update <id> [flags]
```

更新实例

- 类型：`mutation` · 风险：medium

| API | 调用时机 | 用途 |
|---|---|---|
| `ModifyInstanceAttribute` | 指定 `--name` 或指定 `--description` 或指定 `--host-name` 时 | 执行资源操作。 |
| `ModifyInstanceAutoReleaseTime` | 指定 `--auto-release-time` 时 | 执行资源操作。 |
| `ModifyInstanceAutoRenewAttribute` | 指定 `--auto-renew` 或指定 `--auto-renew-period` 时 | 执行资源操作。 |
| `ModifyInstanceChargeType` | 指定 `--instance-charge-type` 且未指定 `--type` 时 | 执行资源操作。 |
| `ModifyInstanceClockOptions` | 指定 `--clock-options` 时 | 执行资源操作。 |
| `ModifyInstanceMaintenanceAttributes` | 指定 `--maintenance-options` 时 | 执行资源操作。 |
| `ModifyInstanceMetadataOptions` | 指定 `--http-endpoint` 或指定 `--http-tokens` 或指定 `--http-put-response-hop-limit` 时 | 执行资源操作。 |
| `ModifyInstanceNetworkOptions` | 指定 `--network-options` 时 | 执行资源操作。 |
| `ModifyInstanceNetworkSpec` | 指定 `--internet-bandwidth-in` 或指定 `--internet-bandwidth-out` 或指定 `--internet-charge-type` 时 | 执行资源操作。 |
| `ModifyInstanceSpec` | 指定 `--type` 且 `--instance-charge-type` 不等于 `PrePaid` 时 | 执行资源操作。 |
| `ModifyPrepayInstanceSpec` | 指定 `--type` 且 `--instance-charge-type` 等于 `PrePaid` 时 | 执行资源操作。 |
| `ModifyInstanceVncPasswd` | 指定 `--vnc-password` 时 | 执行资源操作。 |
| `ModifyInstanceVpcAttribute` | 指定 `--vswitch` 或指定 `--private-ip` 时 | 执行资源操作。 |
| `AllocatePublicIpAddress` | 指定 `--allocate-public-ip` 时 | 执行资源操作。 |
| `ReplaceSystemDisk` | 指定 `--image` 或指定 `--system-disk` 时 | 执行资源操作。 |
| `AttachInstanceRamRole` | 显式将 `--ram-role` 设置为非空值时 | 执行资源操作。 |
| `DetachInstanceRamRole` | 显式将 `--ram-role` 设置为空时 | 执行资源操作。 |
| `AttachKeyPair` | 显式将 `--key-pair` 设置为非空值时 | 执行资源操作。 |
| `DetachKeyPair` | 显式将 `--key-pair` 设置为空时 | 执行资源操作。 |
| `DescribeInstanceAttribute` | 指定 `--security-group-ids` 时 | 读取资源视图。 |
| `JoinSecurityGroup` | 指定 `--security-group-ids` 时 | 执行资源操作。 |
| `LeaveSecurityGroup` | 指定 `--security-group-ids` 时 | 执行资源操作。 |
| `JoinResourceGroup` | 指定 `--resource-group` 时 | 执行资源操作。 |
| `TagResources` | 指定 `--tag` 时 | 执行资源操作。 |
| `UntagResources` | 指定 `--remove-tag` 时 | 执行资源操作。 |
| `DescribeInstances` | 未指定 `--no-wait` 时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--auto-renew` | boolean |  | 自动续费预付费实例 |
| `--clock-options` | object |  | 时钟选项 |
| `--description` | string |  | 实例描述 |
| `--host-name` | string |  | 主机名 |
| `--http-endpoint` | string |  | 元数据服务访问配置 |
| `--http-tokens` | string |  | 元数据服务 Token 要求 |
| `--image` | string |  | ECS 镜像 ID 或名称 |
| `--instance-charge-type` | string |  | 实例付费类型（默认：``） |
| `--internet-bandwidth-out` | integer |  | 公网出带宽 |
| `--key-pair` | string |  | 密钥对名称 |
| `--maintenance-options` | object |  | 维护属性 |
| `--name` | string |  | 实例名称 |
| `--network-options` | object |  | 网络选项 |
| `--period` | integer |  | 预付费周期 |
| `--ram-role` | string |  | RAM 角色名称 |
| `--remove-tag` | string_array |  | 要移除的标签键 |
| `--resource-group` | string |  | 资源组 ID |
| `--security-group-ids` | array |  | 安全组 ID 列表 |
| `--tag` | key_value |  | 标签赋值 key=value |
| `--type` | string |  | 实例规格 |
| `--vswitch` | string |  | 交换机 ID |

## delete

```bash
ecctl ecs instance delete [<ids>...] [flags]
```

删除实例

- 类型：`mutation` · 风险：high
- 同步：等待 `absent`（waiter `deleted_after_delete`，超时 `300s`）；用 `--no-wait` 跳过等待。

| API | 调用时机 | 用途 |
|---|---|---|
| `DeleteInstance` | 只提供一个 `&lt;ids>` 值时 | 执行资源操作。 |
| `DescribeInstances` | 只提供一个 `&lt;ids>` 值且未指定 `--no-wait` 时 | 轮询等待资源达到目标状态。（重复调用） |
| `DeleteInstances` | 提供多个 `&lt;ids>` 值时 | 执行资源操作。 |
| `DescribeInstances` | 提供多个 `&lt;ids>` 值且未指定 `--no-wait` 时 | 轮询等待资源达到目标状态。（重复调用） |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--force` | boolean |  | 强制释放运行中的实例（必须显式指定）（默认：`false`） |

## get

```bash
ecctl ecs instance get <id> [flags]
```

获取实例

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `DescribeInstanceAttribute` | 每次执行命令时 | 读取资源视图。 |
| `DescribeInstanceAutoRenewAttribute` | 指定 `--with-auto-renew` 时 | 读取资源视图。 |
| `DescribeInstanceMaintenanceAttributes` | 指定 `--with-maintenance` 时 | 读取资源视图。 |
| `DescribeInstanceRamRole` | 指定 `--with-ram-role` 时 | 读取资源视图。 |
| `DescribeUserData` | 指定 `--with-user-data` 时 | 读取资源视图。 |
| `DescribeInstanceVncUrl` | 指定 `--with-vnc-url` 时 | 读取资源视图。 |
| `DescribeCloudAssistantStatus` | 指定 `--with-assistant` 时 | 读取资源视图。 |
| `ListPluginStatus` | 指定 `--with-plugin-status` 时 | 读取资源视图。 |
| `ListTagResources` | 指定 `--with-tags` 时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | 要包含的资源字段，使用逗号分隔 |
| `--with-assistant` | boolean |  | 附带云助手状态 |
| `--with-auto-renew` | boolean |  | 附带自动续费设置 |
| `--with-maintenance` | boolean |  | 附带维护属性 |
| `--with-plugin-status` | boolean |  | 附带云助手插件状态 |
| `--with-ram-role` | boolean |  | 附带 RAM 角色信息 |
| `--with-tags` | boolean |  | 附带资源标签 |
| `--with-user-data` | boolean |  | 附带用户数据 |
| `--with-vnc-url` | boolean |  | 附带 VNC 登录地址 |

## list

```bash
ecctl ecs instance list [<ids>...] [flags]
```

列出实例资源

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `DescribeInstances` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | 要包含的资源字段，使用逗号分隔 |
| `--filter` | key_value |  | 过滤表达式 key=value |
| `--limit` | integer |  | 最多返回资源数量（默认：`100`） |
| `--next-token` | string |  | 下一页查询 token |

## exec

```bash
ecctl ecs instance exec [<ids>...] [flags]
```

在实例上执行临时命令

- 类型：`mutation` · 风险：high
- 同步：等待 `Success`（waiter `command_invocation_success`，超时 `600s`）；用 `--no-wait` 跳过等待。

| API | 调用时机 | 用途 |
|---|---|---|
| `RunCommand` | 每次执行命令时 | 执行资源操作。 |
| `DescribeInvocations` | 未指定 `--no-wait` 时 | 轮询等待资源达到目标状态。（重复调用） |
| `DescribeInvocationResults` | 未指定 `--no-wait` 时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--command` | string | ✓ | 在实例上执行的命令内容 |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--command-timeout` | integer |  | 命令超时时间，单位秒 |
| `--command-type` | string |  | 云助手命令类型（默认：`RunShellScript`） |
| `--working-dir` | string |  | 命令工作目录 |

## monitor

```bash
ecctl ecs instance monitor <id> [flags]
```

查询实例监控数据

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `DescribeInstanceMonitorData` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--end-time` | string | ✓ | 监控查询结束时间 |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--start-time` | string | ✓ | 监控查询开始时间 |
| `--monitor-period` | integer |  | 监控数据采样周期，单位秒 |

## reboot

```bash
ecctl ecs instance reboot [<ids>...] [flags]
```

重启实例

- 类型：`mutation` · 风险：medium
- 同步：等待 `Running`（waiter `running_after_reboot`，超时 `300s`）；用 `--no-wait` 跳过等待。

| API | 调用时机 | 用途 |
|---|---|---|
| `RebootInstance` | 只提供一个 `&lt;ids>` 值时 | 执行资源操作。 |
| `DescribeInstances` | 只提供一个 `&lt;ids>` 值且未指定 `--no-wait` 时 | 轮询等待资源达到目标状态。（重复调用） |
| `RebootInstances` | 提供多个 `&lt;ids>` 值时 | 执行资源操作。 |
| `DescribeInstances` | 提供多个 `&lt;ids>` 值且未指定 `--no-wait` 时 | 轮询等待资源达到目标状态。（重复调用） |
| `DescribeInstances` | 未指定 `--no-wait` 时 | 返回最终资源视图。（复用等待结果，不额外请求） |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |

## renew

```bash
ecctl ecs instance renew <id> [flags]
```

续费实例

- 类型：`mutation` · 风险：medium

| API | 调用时机 | 用途 |
|---|---|---|
| `RenewInstance` | 每次执行命令时 | 执行资源操作。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--period` | integer | ✓ | 预付费周期 |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--period-unit` | string |  | 预付费周期单位 |

## sendfile

```bash
ecctl ecs instance sendfile [<ids>...] [flags]
```

向实例下发文件

- 类型：`mutation` · 风险：high
- 同步：等待 `Success`（waiter `sendfile_result_success`，超时 `600s`）；用 `--no-wait` 跳过等待。

| API | 调用时机 | 用途 |
|---|---|---|
| `SendFile` | 每次执行命令时 | 执行资源操作。 |
| `DescribeSendFileResults` | 未指定 `--no-wait` 时 | 轮询等待资源达到目标状态。（重复调用） |
| `DescribeSendFileResults` | 未指定 `--no-wait` 时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--content` | string | ✓ | 要下发的文件内容 |
| `--file-name` | string | ✓ | 要创建的文件名 |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--target-dir` | string | ✓ | 文件目标目录 |
| `--file-mode` | string |  | 文件权限模式 |
| `--group` | string |  | 文件属组 |
| `--owner` | string |  | 文件属主 |

## start

```bash
ecctl ecs instance start [<ids>...] [flags]
```

启动实例

- 类型：`mutation` · 风险：medium
- 同步：等待 `Running`（waiter `running_after_start`，超时 `300s`）；用 `--no-wait` 跳过等待。

| API | 调用时机 | 用途 |
|---|---|---|
| `StartInstance` | 只提供一个 `&lt;ids>` 值时 | 执行资源操作。 |
| `DescribeInstances` | 只提供一个 `&lt;ids>` 值且未指定 `--no-wait` 时 | 轮询等待资源达到目标状态。（重复调用） |
| `StartInstances` | 提供多个 `&lt;ids>` 值时 | 执行资源操作。 |
| `DescribeInstances` | 提供多个 `&lt;ids>` 值且未指定 `--no-wait` 时 | 轮询等待资源达到目标状态。（重复调用） |
| `DescribeInstances` | 未指定 `--no-wait` 时 | 返回最终资源视图。（复用等待结果，不额外请求） |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |

## stop

```bash
ecctl ecs instance stop [<ids>...] [flags]
```

停止实例

- 类型：`mutation` · 风险：medium
- 同步：等待 `Stopped`（waiter `stopped_after_stop`，超时 `300s`）；用 `--no-wait` 跳过等待。

| API | 调用时机 | 用途 |
|---|---|---|
| `StopInstance` | 只提供一个 `&lt;ids>` 值时 | 执行资源操作。 |
| `DescribeInstances` | 只提供一个 `&lt;ids>` 值且未指定 `--no-wait` 时 | 轮询等待资源达到目标状态。（重复调用） |
| `StopInstances` | 提供多个 `&lt;ids>` 值时 | 执行资源操作。 |
| `DescribeInstances` | 提供多个 `&lt;ids>` 值且未指定 `--no-wait` 时 | 轮询等待资源达到目标状态。（重复调用） |
| `DescribeInstances` | 未指定 `--no-wait` 时 | 返回最终资源视图。（复用等待结果，不额外请求） |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |

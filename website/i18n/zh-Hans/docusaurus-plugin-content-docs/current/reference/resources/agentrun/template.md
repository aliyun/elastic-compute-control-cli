---
generated: true
generated_by: "website/scripts/gen-reference.mjs"
generated_command: "make build && npm --prefix website run gen:reference"
title: agentrun template
sidebar_label: template
description: "管理 AgentRun 沙箱模板及其 MCP 服务。"
---

# agentrun template

管理 AgentRun 沙箱模板及其 MCP 服务。

运行 `ecctl agentrun template <action> -h` 查看用法，或 `ecctl schema agentrun.template.<action> --full` 获取该命令完整的结构化规格——每个参数与行为，便于 Agent 读取和填参。

## create

```bash
ecctl agentrun template create [flags]
```

创建 AgentRun 沙箱模板

- 类型：`mutation` · 风险：medium
- 同步：等待 `READY`（waiter `ready_after_change`，超时 `600s`）；用 `--no-wait` 跳过等待。

| API | 调用时机 | 用途 |
|---|---|---|
| `CreateTemplate` | 每次执行命令时 | 执行资源操作。 |
| `GetTemplate` | 未指定 `--no-wait` 时 | 轮询等待资源达到目标状态。（重复调用） |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--cpu` | number | ✓ | CPU 核数 |
| `--memory` | integer | ✓ | 内存大小，单位 MB |
| `--name` | string | ✓ | 模板名称 |
| `--network-configuration` | object | ✓ | 网络配置，使用 JSON 对象或 @file |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--template-type` | string | ✓ | 模板类型 |
| `--allow-anonymous-manage` | boolean |  | 允许数据链路调用沙箱创建、停止和删除接口 |
| `--arms-configuration` | object |  | ARMS 配置，使用 JSON 对象或 @file |
| `--container-configuration` | object |  | 容器配置，使用 JSON 对象或 @file |
| `--credential-configuration` | object |  | 凭证配置，使用 JSON 对象或 @file |
| `--description` | string |  | 模板描述 |
| `--disk-size` | integer |  | 磁盘大小，单位 MB |
| `--enable-agent` | boolean |  | 启用 Sandbox Agent |
| `--enable-pre-stop` | boolean |  | 启用停止前处理 |
| `--environment-variables` | object |  | 环境变量，使用 JSON 对象或 @file |
| `--execution-role-arn` | string |  | 执行角色 ARN |
| `--idle-timeout` | integer |  | 沙箱空闲超时时间，单位秒 |
| `--log-configuration` | object |  | 日志配置，使用 JSON 对象或 @file |
| `--nas-config` | object |  | NAS 挂载配置，使用 JSON 对象或 @file |
| `--oss-configuration` | object |  | OSS 挂载配置项；每个挂载使用一个 JSON 对象或 @file，并重复指定该参数 |
| `--pre-stop-timeout` | integer |  | 停止前处理超时时间，单位秒 |
| `--scaling-config` | object |  | 弹性配置，使用 JSON 对象或 @file |
| `--template-configuration` | object |  | 模板类型相关配置，使用 JSON 对象或 @file |
| `--workspace` | string |  | 工作空间 ID |

## update

```bash
ecctl agentrun template update <name> [flags]
```

更新 AgentRun 沙箱模板

- 类型：`mutation` · 风险：medium
- 同步：等待 `READY`（waiter `ready_after_change`，超时 `600s`）；用 `--no-wait` 跳过等待。
- 通过 `clientToken` 幂等。

| API | 调用时机 | 用途 |
|---|---|---|
| `UpdateTemplate` | 每次执行命令时 | 执行资源操作。 |
| `GetTemplate` | 未指定 `--no-wait` 时 | 轮询等待资源达到目标状态。（重复调用） |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--allow-anonymous-manage` | boolean |  | 允许数据链路调用沙箱创建、停止和删除接口 |
| `--arms-configuration` | object |  | ARMS 配置，使用 JSON 对象或 @file |
| `--container-configuration` | object |  | 容器配置，使用 JSON 对象或 @file |
| `--cpu` | number |  | CPU 核数 |
| `--credential-configuration` | object |  | 凭证配置，使用 JSON 对象或 @file |
| `--description` | string |  | 模板描述 |
| `--enable-agent` | boolean |  | 启用 Sandbox Agent |
| `--enable-pre-stop` | boolean |  | 启用停止前处理 |
| `--environment-variables` | object |  | 环境变量，使用 JSON 对象或 @file |
| `--execution-role-arn` | string |  | 执行角色 ARN |
| `--idle-timeout` | integer |  | 沙箱空闲超时时间，单位秒 |
| `--log-configuration` | object |  | 日志配置，使用 JSON 对象或 @file |
| `--memory` | integer |  | 内存大小，单位 MB |
| `--nas-config` | object |  | NAS 挂载配置，使用 JSON 对象或 @file |
| `--network-configuration` | object |  | 网络配置，使用 JSON 对象或 @file |
| `--oss-configuration` | object |  | OSS 挂载配置项；每个挂载使用一个 JSON 对象或 @file，并重复指定该参数 |
| `--pre-stop-timeout` | integer |  | 停止前处理超时时间，单位秒 |
| `--scaling-config` | object |  | 弹性配置，使用 JSON 对象或 @file |
| `--template-configuration` | object |  | 模板类型相关配置，使用 JSON 对象或 @file |
| `--workspace` | string |  | 工作空间 ID |

## delete

```bash
ecctl agentrun template delete <name> [flags]
```

删除 AgentRun 沙箱模板

- 类型：`mutation` · 风险：high
- 同步：等待 `absent`（waiter `absent_after_delete`，超时 `300s`）；用 `--no-wait` 跳过等待。

| API | 调用时机 | 用途 |
|---|---|---|
| `DeleteTemplate` | 每次执行命令时 | 执行资源操作。 |
| `ListTemplates` | 未指定 `--no-wait` 时 | 轮询等待资源达到目标状态。（重复调用） |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |

## get

```bash
ecctl agentrun template get <name> [flags]
```

获取 AgentRun 沙箱模板

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `GetTemplate` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | 要包含的资源字段，使用逗号分隔 |

## list

```bash
ecctl agentrun template list [flags]
```

列出 AgentRun 沙箱模板

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `ListTemplates` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | 要包含的资源字段，使用逗号分隔 |
| `--filter` | key_value |  | 过滤表达式 key=value |
| `--limit` | integer |  | 最多返回的模板数量（默认：`100`） |
| `--page` | integer |  | 返回结果页码（默认：`1`） |
| `--workspace-ids` | string |  | AgentRun 接受的工作空间 ID 列表过滤值 |

## disable

```bash
ecctl agentrun template disable <name> [flags]
```

停用沙箱模板的 MCP 服务

- 类型：`mutation` · 风险：medium

| API | 调用时机 | 用途 |
|---|---|---|
| `StopTemplateMCP` | 每次执行命令时 | 执行资源操作。 |
| `GetTemplate` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |

## enable

```bash
ecctl agentrun template enable <name> [flags]
```

启用沙箱模板的 MCP 服务

- 类型：`mutation` · 风险：medium

| API | 调用时机 | 用途 |
|---|---|---|
| `ActivateTemplateMCP` | 每次执行命令时 | 执行资源操作。 |
| `GetTemplate` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--enabled-tools` | array |  | 要启用的 MCP 工具 |
| `--transport` | string |  | MCP 传输协议 |

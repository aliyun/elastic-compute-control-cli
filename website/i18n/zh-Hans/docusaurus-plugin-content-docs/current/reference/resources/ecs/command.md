---
generated: true
generated_by: "website/scripts/gen-reference.mjs"
generated_command: "make build && npm --prefix website run gen:reference"
title: ecs command
sidebar_label: command
description: "管理 ECS 云助手命令模板与执行记录"
---

# ecs command

管理 ECS 云助手命令模板与执行记录

运行 `ecctl ecs command <action> -h` 查看用法，或 `ecctl schema ecs.command.<action> --full` 获取该命令完整的结构化规格——每个参数与行为，便于 Agent 读取和填参。

## create

```bash
ecctl ecs command create [flags]
```

创建云助手命令模板

- 类型：`mutation` · 风险：medium
- 通过 `ClientToken` 幂等。

| API | 调用时机 | 用途 |
|---|---|---|
| `CreateCommand` | 每次执行命令时 | 执行资源操作。 |
| `DescribeCommands` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--command-content` | string | ✓ | 在实例上执行的命令内容 |
| `--name` | string | ✓ | 云助手命令名称 |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--type` | string | ✓ | 云助手命令类型 |
| `--command-timeout` | integer |  | 命令超时时间，单位秒 |
| `--content-encoding` | string |  | 命令内容编码方式 |
| `--description` | string |  | 云助手命令描述 |
| `--enable-parameter` | boolean |  | 启用命令中的自定义参数 |
| `--launcher` | string |  | 命令启动器 |
| `--resource-group` | string |  | 资源组 ID |
| `--tag` | key_value |  | 标签赋值 key=value |
| `--working-dir` | string |  | 命令工作目录 |

## update

```bash
ecctl ecs command update <id> [flags]
```

更新命令模板或执行记录属性

- 类型：`mutation` · 风险：medium

| API | 调用时机 | 用途 |
|---|---|---|
| `ModifyInvocationAttribute` | 指定 `--invocation-id` 时 | 执行资源操作。 |
| `ModifyCommand` | 指定 `&lt;id>` 时 | 执行资源操作。 |
| `DescribeInvocations` | 指定 `--invocation-id` 且未指定 `--no-wait` 时 | 读取资源视图。 |
| `DescribeCommands` | 指定 `&lt;id>` 且未指定 `--no-wait` 时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--command-content` | string |  | 在实例上执行的命令内容 |
| `--command-timeout` | integer |  | 命令超时时间，单位秒 |
| `--description` | string |  | 云助手命令描述 |
| `--frequency` | string |  | 定时执行频率（cron 表达式） |
| `--invocation-id` | string |  | 执行记录 ID |
| `--launcher` | string |  | 命令启动器 |
| `--name` | string |  | 云助手命令名称 |
| `--repeat-mode` | string |  | 执行重复模式 |
| `--timed` | boolean |  | 是否为定时执行 |
| `--working-dir` | string |  | 命令工作目录 |

## delete

```bash
ecctl ecs command delete <id> [flags]
```

删除命令模板

- 类型：`mutation` · 风险：high
- 同步：等待 `absent`（waiter `deleted_after_delete`，超时 `120s`）；用 `--no-wait` 跳过等待。

| API | 调用时机 | 用途 |
|---|---|---|
| `DeleteCommand` | 每次执行命令时 | 执行资源操作。 |
| `DescribeCommands` | 未指定 `--no-wait` 时 | 轮询等待资源达到目标状态。（重复调用） |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |

## get

```bash
ecctl ecs command get <id> [flags]
```

获取命令模板、执行记录或执行结果

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `DescribeInvocations` | 指定 `--invocation-id` 时 | 读取资源视图。 |
| `DescribeInvocationResults` | 指定 `--with-results` 时 | 读取资源视图。 |
| `DescribeCommands` | 指定 `&lt;id>` 时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | 要包含的资源字段，使用逗号分隔 |
| `--include-history` | boolean |  | 返回历史执行结果 |
| `--include-output` | boolean |  | 返回执行输出内容 |
| `--instance` | string |  | 实例 ID 过滤 |
| `--invocation-id` | string |  | 执行记录 ID |
| `--invoke-record-status` | string |  | 执行结果状态过滤 |
| `--with-results` | boolean |  | 返回执行结果 |

## list

```bash
ecctl ecs command list <id> [flags]
```

列出命令模板或执行记录

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `DescribeInvocations` | 指定 `--invocations` 时 | 读取资源视图。 |
| `DescribeCommands` | 未指定 `--invocations` 时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--content-encoding` | string |  | 命令内容编码方式 |
| `--fields` | string |  | 要包含的资源字段，使用逗号分隔 |
| `--filter` | key_value |  | 过滤表达式 key=value |
| `--include-output` | boolean |  | 返回执行输出内容 |
| `--invocations` | boolean |  | 列出执行记录而非命令模板 |
| `--limit` | integer |  | 最多返回资源数量（默认：`50`） |
| `--next-token` | string |  | 下一页查询 token |

## invoke

```bash
ecctl ecs command invoke <id> [flags]
```

在目标实例上执行命令

- 类型：`mutation` · 风险：high
- 同步：等待 `Finished`（waiter `finished_after_invoke`，超时 `600s`）；用 `--no-wait` 跳过等待。
- 通过 `ClientToken` 幂等。

| API | 调用时机 | 用途 |
|---|---|---|
| `InvokeCommand` | 每次执行命令时 | 执行资源操作。 |
| `DescribeInvocations` | 未指定 `--no-wait` 时 | 轮询等待资源达到目标状态。（重复调用） |
| `DescribeInvocationResults` | 未指定 `--no-wait` 时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--instance-ids` | array | ✓ | 目标实例 ID 列表 |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--container-id` | string |  | 容器 ID |
| `--container-name` | string |  | 容器名称 |
| `--frequency` | string |  | 定时执行频率（cron 表达式） |
| `--parameters` | string |  | 命令参数（JSON 对象） |
| `--repeat-mode` | string |  | 执行重复模式 |
| `--resource-group` | string |  | 资源组 ID |
| `--tag` | key_value |  | 标签赋值 key=value |
| `--timed` | boolean |  | 是否为定时执行 |
| `--username` | string |  | 执行命令的用户名 |
| `--windows-password-name` | string |  | Windows 加密密码键名 |

## stop

```bash
ecctl ecs command stop <invocation-id> [flags]
```

停止正在执行的命令

- 类型：`mutation` · 风险：medium
- 同步：等待 `Stopped`（waiter `stopped_after_stop`，超时 `300s`）；用 `--no-wait` 跳过等待。

| API | 调用时机 | 用途 |
|---|---|---|
| `StopInvocation` | 每次执行命令时 | 执行资源操作。 |
| `DescribeInvocations` | 未指定 `--no-wait` 时 | 轮询等待资源达到目标状态。（重复调用） |
| `DescribeInvocations` | 未指定 `--no-wait` 时 | 读取资源视图。 |
| `DescribeInvocationResults` | 未指定 `--no-wait` 时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--force` | boolean |  | 在 API 支持时强制停止执行（必须显式指定）（默认：`false`） |
| `--instance-ids` | array |  | 目标实例 ID 列表 |

---
generated: true
generated_by: "website/scripts/gen-reference.mjs"
generated_command: "make build && npm --prefix website run gen:reference"
title: agentrun sandbox
sidebar_label: sandbox
description: "管理隔离的 AgentRun 沙箱实例。"
---

# agentrun sandbox

管理隔离的 AgentRun 沙箱实例。

运行 `ecctl agentrun sandbox <action> -h` 查看用法，或 `ecctl schema agentrun.sandbox.<action> --full` 获取该命令完整的结构化规格——每个参数与行为，便于 Agent 读取和填参。

## create

```bash
ecctl agentrun sandbox create [flags]
```

创建 AgentRun 沙箱

- 类型：`mutation` · 风险：medium
- 同步：等待 `READY`（waiter `ready_after_create`，超时 `600s`）；用 `--no-wait` 跳过等待。

| API | 调用时机 | 用途 |
|---|---|---|
| `CreateSandbox` | 每次执行命令时 | 执行资源操作。 |
| `GetSandbox` | 未指定 `--no-wait` 时 | 轮询等待资源达到目标状态。（重复调用） |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--template-name` | string | ✓ | 模板名称 |
| `--id` | string |  | 沙箱 ID |
| `--idle-timeout` | integer |  | 沙箱空闲超时时间，单位秒 |
| `--nas-config` | object |  | NAS 配置，使用 JSON 对象或 @file |
| `--oss-mount-config` | object |  | OSS 挂载配置，使用 JSON 对象或 @file |
| `--polar-fs-config` | object |  | PolarFS 配置，使用 JSON 对象或 @file |

## delete

```bash
ecctl agentrun sandbox delete <id> [flags]
```

删除 AgentRun 沙箱

- 类型：`mutation` · 风险：high
- 同步：等待 `absent`（waiter `absent_after_delete`，超时 `300s`）；用 `--no-wait` 跳过等待。

| API | 调用时机 | 用途 |
|---|---|---|
| `DeleteSandbox` | 每次执行命令时 | 执行资源操作。 |
| `ListSandboxes` | 未指定 `--no-wait` 时 | 轮询等待资源达到目标状态。（重复调用） |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |

## get

```bash
ecctl agentrun sandbox get <id> [flags]
```

获取 AgentRun 沙箱

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `GetSandbox` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | 要包含的资源字段，使用逗号分隔 |

## list

```bash
ecctl agentrun sandbox list [flags]
```

列出 AgentRun 沙箱

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `ListSandboxes` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | 要包含的资源字段，使用逗号分隔 |
| `--filter` | key_value |  | 过滤表达式 key=value |
| `--limit` | integer |  | 最多返回的沙箱数量（默认：`100`） |
| `--next-token` | string |  | 上一页响应返回的分页令牌 |

## stop

```bash
ecctl agentrun sandbox stop <id> [flags]
```

停止 AgentRun 沙箱

- 类型：`mutation` · 风险：medium
- 同步：等待 `TERMINATED`（waiter `terminated_after_stop`，超时 `300s`）；用 `--no-wait` 跳过等待。

| API | 调用时机 | 用途 |
|---|---|---|
| `StopSandbox` | 每次执行命令时 | 执行资源操作。 |
| `GetSandbox` | 未指定 `--no-wait` 时 | 轮询等待资源达到目标状态。（重复调用） |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |

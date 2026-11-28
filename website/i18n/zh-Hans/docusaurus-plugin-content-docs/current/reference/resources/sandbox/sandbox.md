---
generated: true
generated_by: "website/scripts/gen-reference.mjs"
generated_command: "make build && npm --prefix website run gen:reference"
title: sandbox
sidebar_label: sandbox
description: "管理 E2B 沙箱的生命周期和控制面状态。"
---

# sandbox

管理 E2B 沙箱的生命周期和控制面状态。

运行 `ecctl sandbox <action> -h` 查看用法，或 `ecctl schema sandbox.sandbox.<action> --full` 获取该命令完整的结构化规格——每个参数与行为，便于 Agent 读取和填参。

## create

```bash
ecctl sandbox create [flags]
```

创建 E2B 沙箱

- 类型：`mutation` · 风险：medium
- 同步：等待 `running`（waiter `running`，超时 `300s`）；用 `--no-wait` 跳过等待。

| API | 调用时机 | 用途 |
|---|---|---|
| `CreateSandbox` | 每次执行命令时 | 执行资源操作。 |
| `GetSandbox` | 未指定 `--no-wait` 时 | 轮询等待资源达到目标状态。（重复调用） |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--template` | string | ✓ | 模板 ID 或别名 |
| `--allow-internet-access` | boolean |  | 允许沙箱访问互联网 |
| `--auto-pause` | boolean |  | 超时后自动暂停沙箱 |
| `--auto-pause-memory` | boolean |  | 自动暂停时保留内存状态 |
| `--auto-resume` | object |  | 自动恢复配置，使用 JSON 或 @file |
| `--env-vars` | object |  | 环境变量，使用 JSON 或 @file |
| `--iam` | object |  | 工作负载身份配置，使用 JSON 或 @file |
| `--mcp` | object |  | MCP 配置，使用 JSON 或 @file |
| `--metadata` | object |  | 沙箱元数据，使用 JSON 或 @file |
| `--network` | object |  | 网络配置，使用 JSON 或 @file |
| `--secure` | boolean |  | 保护与沙箱的系统通信 |
| `--timeout-seconds` | integer |  | 生存时间（秒）（默认：`300`） |
| `--volume-mount` | object |  | 卷挂载，使用 JSON 数组或 @file |

## update

```bash
ecctl sandbox update <id> [flags]
```

更新 E2B 沙箱的超时或网络配置

- 类型：`mutation` · 风险：medium

| API | 调用时机 | 用途 |
|---|---|---|
| `UpdateSandboxTimeout` | 指定 `--timeout-seconds` 时 | 执行资源操作。 |
| `UpdateSandboxNetwork` | 指定 `--network` 时 | 执行资源操作。 |
| `GetSandbox` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--network` | object |  | 网络配置，使用 JSON 或 @file |
| `--timeout-seconds` | integer |  | 生存时间（秒） |

## delete

```bash
ecctl sandbox delete <id> [flags]
```

终止 E2B 沙箱

- 类型：`mutation` · 风险：high
- 同步：等待 `absent`（waiter `deleted`，超时 `300s`）；用 `--no-wait` 跳过等待。

| API | 调用时机 | 用途 |
|---|---|---|
| `KillSandbox` | 每次执行命令时 | 执行资源操作。 |
| `GetSandbox` | 未指定 `--no-wait` 时 | 轮询等待资源达到目标状态。（重复调用） |

## get

```bash
ecctl sandbox get <id> [flags]
```

获取 E2B 沙箱

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `GetSandbox` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--fields` | string |  | 要包含的资源字段，使用逗号分隔 |

## list

```bash
ecctl sandbox list [flags]
```

列出 E2B 沙箱

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `ListSandboxes` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--fields` | string |  | 要包含的资源字段，使用逗号分隔 |
| `--filter` | key_value |  | 过滤表达式 key=value |
| `--limit` | integer |  | 最多返回结果数（默认：`100`） |
| `--next-token` | string |  | 分页令牌 |

## fork

```bash
ecctl sandbox fork <id> [flags]
```

分叉 E2B 沙箱

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `ForkSandbox` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--count` | integer |  | 分叉沙箱数量（默认：`1`） |
| `--timeout-seconds` | integer |  | 生存时间（秒）（默认：`300`） |

## logs

```bash
ecctl sandbox logs <id> [flags]
```

读取 E2B 沙箱日志

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `GetSandboxLogs` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--cursor` | integer |  | 日志游标（毫秒） |
| `--direction` | string |  | 日志方向 |
| `--level` | string |  | 最低日志级别 |
| `--limit` | integer |  | 最多返回沙箱日志数（默认：`1000`） |
| `--search` | string |  | 日志消息子串 |

## metrics

```bash
ecctl sandbox metrics <id> [flags]
```

读取 E2B 沙箱指标

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `GetSandboxMetrics` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--end` | integer |  | 指标结束时间（Unix 秒） |
| `--start` | integer |  | 指标开始时间（Unix 秒） |

## pause

```bash
ecctl sandbox pause <id> [flags]
```

暂停 E2B 沙箱

- 类型：`mutation` · 风险：medium
- 同步：等待 `paused`（waiter `paused`，超时 `300s`）；用 `--no-wait` 跳过等待。

| API | 调用时机 | 用途 |
|---|---|---|
| `PauseSandbox` | 每次执行命令时 | 执行资源操作。 |
| `GetSandbox` | 未指定 `--no-wait` 时 | 轮询等待资源达到目标状态。（重复调用） |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--memory` | boolean |  | 暂停时保留内存状态（默认：`true`） |

## refresh

```bash
ecctl sandbox refresh <id> [flags]
```

延长 E2B 沙箱生存时间

- 类型：`mutation` · 风险：medium

| API | 调用时机 | 用途 |
|---|---|---|
| `RefreshSandbox` | 每次执行命令时 | 执行资源操作。 |
| `GetSandbox` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--duration` | integer | ✓ | 续期时长（秒） |

## resume

```bash
ecctl sandbox resume <id> [flags]
```

通过控制面恢复 E2B 沙箱

- 类型：`mutation` · 风险：medium
- 同步：等待 `running`（waiter `running`，超时 `300s`）；用 `--no-wait` 跳过等待。

| API | 调用时机 | 用途 |
|---|---|---|
| `ConnectSandbox` | 每次执行命令时 | 执行资源操作。 |
| `GetSandbox` | 未指定 `--no-wait` 时 | 轮询等待资源达到目标状态。（重复调用） |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--timeout-seconds` | integer |  | 生存时间（秒）（默认：`300`） |

## snapshot

```bash
ecctl sandbox snapshot <id> [flags]
```

从 E2B 沙箱创建持久快照

- 类型：`mutation` · 风险：medium

| API | 调用时机 | 用途 |
|---|---|---|
| `CreateSandboxSnapshot` | 每次执行命令时 | 执行资源操作。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--snapshot-name` | string |  | 快照名称 |

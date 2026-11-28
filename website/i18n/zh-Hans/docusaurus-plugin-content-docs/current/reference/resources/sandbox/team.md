---
generated: true
generated_by: "website/scripts/gen-reference.mjs"
generated_command: "make build && npm --prefix website run gen:reference"
title: sandbox team
sidebar_label: team
description: "管理 FC 云沙箱团队。"
---

# sandbox team

管理 FC 云沙箱团队。

运行 `ecctl sandbox team <action> -h` 查看用法，或 `ecctl schema sandbox.team.<action> --full` 获取该命令完整的结构化规格——每个参数与行为，便于 Agent 读取和填参。

## create

```bash
ecctl sandbox team create [flags]
```

创建 FC 云沙箱团队

- 类型：`mutation` · 风险：medium

| API | 调用时机 | 用途 |
|---|---|---|
| `CreateTeam` | 每次执行命令时 | 执行资源操作。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--name` | string | ✓ | 团队名称 |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--description` | string |  | 团队描述 |
| `--plan` | string |  | 团队订阅计划 |
| `--resource-group` | string |  | 资源组 ID |

## update

```bash
ecctl sandbox team update <id> [flags]
```

更新 FC 云沙箱团队

- 类型：`mutation` · 风险：medium

| API | 调用时机 | 用途 |
|---|---|---|
| `GetTeam` | 每次执行命令时 | 检查只读和团队改名限制 |
| `UpdateTeam` | 每次执行命令时 | 执行资源操作。 |
| `GetTeam` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--description` | string |  | 团队描述 |
| `--name` | string |  | 团队名称 |
| `--plan` | string |  | 团队订阅计划 |
| `--resource-group` | string |  | 资源组 ID |

## delete

```bash
ecctl sandbox team delete <id> [flags]
```

删除 FC 云沙箱团队

- 类型：`mutation` · 风险：high
- 同步：等待 `absent`（waiter `absent_after_delete`，超时 `300s`）；用 `--no-wait` 跳过等待。

| API | 调用时机 | 用途 |
|---|---|---|
| `GetTeam` | 每次执行命令时 | 检查团队是否只读 |
| `DeleteTeam` | 每次执行命令时 | 执行资源操作。 |
| `GetTeam` | 未指定 `--no-wait` 时 | 轮询等待资源达到目标状态。（重复调用） |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |

## get

```bash
ecctl sandbox team get <id> [flags]
```

获取 FC 云沙箱团队

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `GetTeam` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | 要包含的资源字段，使用逗号分隔 |

## list

```bash
ecctl sandbox team list [flags]
```

列出 FC 云沙箱团队

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `ListTeams` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--all` | boolean |  | 从第一页获取所有结果 |
| `--fields` | string |  | 要包含的资源字段，使用逗号分隔 |
| `--filter` | key_value |  | 过滤表达式 key=value |
| `--limit` | integer |  | 每页最多返回的团队数量（默认：`50`） |
| `--page` | integer |  | 返回结果页码（默认：`1`） |

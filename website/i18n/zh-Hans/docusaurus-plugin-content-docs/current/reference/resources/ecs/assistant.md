---
generated: true
generated_by: "website/scripts/gen-reference.mjs"
generated_command: "make build && npm --prefix website run gen:reference"
title: ecs assistant
sidebar_label: assistant
description: "管理云助手服务配置与 Agent 安装"
---

# ecs assistant

管理云助手服务配置与 Agent 安装

运行 `ecctl ecs assistant <action> -h` 查看用法，或 `ecctl schema ecs.assistant.<action> --full` 获取该命令完整的结构化规格——每个参数与行为，便于 Agent 读取和填参。

## update

```bash
ecctl ecs assistant update [flags]
```

修改云助手服务配置

- 类型：`mutation` · 风险：medium

| API | 调用时机 | 用途 |
|---|---|---|
| `ModifyCloudAssistantSettings` | 每次执行命令时 | 执行资源操作。 |
| `DescribeCloudAssistantSettings` | 未指定 `--no-wait` 时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--setting-type` | string | ✓ | 要修改的云助手配置类型 |

## get

```bash
ecctl ecs assistant get [flags]
```

查询云助手服务配置

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `DescribeCloudAssistantSettings` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | 要包含的资源字段，使用逗号分隔 |
| `--setting-types` | array |  | 要查询的云助手配置类型（DescribeCloudAssistantSettings 必须至少指定一个）（默认：`["InvocationDelivery","SessionManagerDelivery","AgentUpgradeConfig","SessionManagerConfig"]`） |

## install

```bash
ecctl ecs assistant install [flags]
```

为实例安装云助手 Agent

- 类型：`mutation` · 风险：medium
- 同步：等待 `true`（waiter `assistant_available`，超时 `300s`）；用 `--no-wait` 跳过等待。

| API | 调用时机 | 用途 |
|---|---|---|
| `InstallCloudAssistant` | 每次执行命令时 | 执行资源操作。 |
| `DescribeCloudAssistantStatus` | 未指定 `--no-wait` 时 | 轮询等待资源达到目标状态。（重复调用） |
| `DescribeCloudAssistantStatus` | 未指定 `--no-wait` 时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--instance-ids` | array | ✓ | 要安装云助手 Agent 的实例 ID 列表 |
| `--region` | string | ✓ | Alibaba Cloud region |

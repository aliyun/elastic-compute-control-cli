---
title: ecs keypair
sidebar_label: keypair
description: "管理 SSH 密钥对"
---

# ecs keypair

管理 SSH 密钥对

运行 `ecctl ecs keypair <action> -h` 查看用法，或 `ecctl schema ecs.keypair.<action> --full` 获取该命令完整的结构化规格——每个参数与行为，便于 Agent 读取和填参。

## create

```bash
ecctl ecs keypair create [flags]
```

创建或导入密钥对

- 类型：`mutation` · 风险：medium

| API | 调用时机 | 用途 |
|---|---|---|
| `ImportKeyPair` | 指定 `--public-key` 时 | 执行资源操作。 |
| `CreateKeyPair` | 未指定 `--public-key` 时 | 执行资源操作。 |
| `DescribeKeyPairs` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--name` | string | ✓ | 密钥对名称 |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--public-key` | string |  | 要导入的已有公钥（创建时切换为导入密钥对） |
| `--resource-group` | string |  | 资源组 ID |
| `--tag` | key_value |  | 标签赋值 key=value |

## delete

```bash
ecctl ecs keypair delete [<ids>...] [flags]
```

删除密钥对

- 类型：`mutation` · 风险：high

| API | 调用时机 | 用途 |
|---|---|---|
| `DeleteKeyPairs` | 每次执行命令时 | 执行资源操作。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |

## get

```bash
ecctl ecs keypair get <id> [flags]
```

获取密钥对

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `DescribeKeyPairs` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | 要包含的资源字段，使用逗号分隔 |

## list

```bash
ecctl ecs keypair list <id> [flags]
```

列出密钥对

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `DescribeKeyPairs` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | 要包含的资源字段，使用逗号分隔 |
| `--filter` | key_value |  | 过滤表达式 key=value |
| `--limit` | integer |  | 最多返回资源数量（默认：`50`） |
| `--page` | integer |  | 返回结果页码（默认：`1`） |

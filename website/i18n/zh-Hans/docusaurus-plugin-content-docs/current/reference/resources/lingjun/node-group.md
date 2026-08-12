---
generated: true
generated_by: "website/scripts/gen-reference.mjs"
generated_command: "make build && npm --prefix website run gen:reference"
title: lingjun node-group
sidebar_label: node-group
description: "管理灵骏节点组资源"
---

# lingjun node-group

管理灵骏节点组资源

运行 `ecctl lingjun node-group <action> -h` 查看用法，或 `ecctl schema lingjun.node-group.<action> --full` 获取该命令完整的结构化规格——每个参数与行为，便于 Agent 读取和填参。

## create

```bash
ecctl lingjun node-group create [flags]
```

创建灵骏节点组

- 类型：`mutation` · 风险：medium

| API | 调用时机 | 用途 |
|---|---|---|
| `CreateNodeGroup` | 每次执行命令时 | 执行资源操作。 |
| `DescribeNodeGroup` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--cluster` | string | ✓ | 所属集群 ID |
| `--name` | string | ✓ | 节点组名称 |
| `--node-group` | string | ✓ | 节点组配置 JSON 对象，结构与 CreateNodeGroup 的 NodeGroup body 一致（例如 &#123;"NodeGroupName":"...","Az":"...","MachineType":"...","ImageId":"...",...}）；支持内联 JSON 或 @file 路径 |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--node-unit` | string |  | 节点单元配置 JSON 对象，结构与 CreateNodeGroup 的 NodeUnit body 一致（例如 &#123;"NodeUnitId":"...","NodeUnitName":"...","ResourceGroupId":"...","MaxNodes":...}）；支持内联 JSON 或 @file 路径 |

## update

```bash
ecctl lingjun node-group update <id> [flags]
```

更新灵骏节点组

- 类型：`mutation` · 风险：medium

| API | 调用时机 | 用途 |
|---|---|---|
| `UpdateNodeGroup` | 每次执行命令时 | 执行资源操作。 |
| `DescribeNodeGroup` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--file-system-mount-enabled` | boolean |  | 启用文件系统挂载 |
| `--image` | string |  | 镜像 ID |
| `--key-pair` | string |  | 密钥对名称 |
| `--login-password` | string |  | 登录密码（敏感：建议优先使用 --key-pair；通过 @file 或环境变量注入，避免出现在 shell 历史记录中） |
| `--name` | string |  | 节点组名称 |
| `--ram-role` | string |  | RAM 角色名称 |
| `--user-data` | string |  | 用户自定义脚本 |

## delete

```bash
ecctl lingjun node-group delete <id> [flags]
```

删除灵骏节点组

- 类型：`mutation` · 风险：high
- 同步：等待 `absent`（waiter `deleted_after_delete`，超时 `600s`）；用 `--no-wait` 跳过等待。

| API | 调用时机 | 用途 |
|---|---|---|
| `DeleteNodeGroup` | 每次执行命令时 | 执行资源操作。 |
| `DescribeNodeGroup` | 未指定 `--no-wait` 时 | 轮询等待资源达到目标状态。（重复调用） |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--cluster` | string |  | 所属集群 ID |

## get

```bash
ecctl lingjun node-group get <id> [flags]
```

查询灵骏节点组详情

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `DescribeNodeGroup` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | 要包含的资源字段，使用逗号分隔 |

## list

```bash
ecctl lingjun node-group list [flags]
```

列出灵骏节点组

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `ListNodeGroups` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--cluster` | string |  | 所属集群 ID |
| `--fields` | string |  | 要包含的资源字段，使用逗号分隔 |
| `--filter` | key_value |  | 过滤表达式 key=value |
| `--limit` | integer |  | 最多返回资源数量（默认：`100`） |
| `--next-token` | string |  | 下一页查询 token |

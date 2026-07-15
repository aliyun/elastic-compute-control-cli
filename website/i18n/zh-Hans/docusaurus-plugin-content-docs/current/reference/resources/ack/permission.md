---
title: ack permission
sidebar_label: permission
description: "管理 ACK RAM 用户和角色权限"
---

# ack permission

管理 ACK RAM 用户和角色权限

运行 `ecctl ack permission <action> -h` 查看用法，或 `ecctl schema ack.permission.<action> --full` 获取该命令完整的结构化规格——每个参数与行为，便于 Agent 读取和填参。

## update

```bash
ecctl ack permission update [flags]
```

更新或覆盖 RAM 用户/角色的 ACK RBAC 权限

- 类型：`mutation` · 风险：medium

| API | 调用时机 | 用途 |
|---|---|---|
| `UpdateUserPermissions` | 未指定 `--replace` 时 | 执行资源操作。 |
| `GrantPermissions` | 指定 `--replace` 时 | 执行资源操作。 |
| `DescribeUserPermission` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--permission` | object | ✓ | 权限授权项，每个 flag 传一个内联 key=value、JSON 对象或 @file。 |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--user-id` | string | ✓ | RAM 用户或 RAM 角色 ID |
| `--mode` | string |  | UpdateUserPermissions 的增量更新模式（默认：`patch`） |
| `--replace` | boolean |  | 使用 GrantPermissions 覆盖该用户全部授权，而不是增量调用 UpdateUserPermissions |

## delete

```bash
ecctl ack permission delete [flags]
```

撤销 RAM 用户/角色的 ACK RBAC 权限和 KubeConfig 访问

- 类型：`mutation` · 风险：high

| API | 调用时机 | 用途 |
|---|---|---|
| `CleanClusterUserPermissions` | 未指定 `--all-clusters` 时 | 执行资源操作。 |
| `CleanUserPermissions` | 指定 `--all-clusters` 时 | 执行资源操作。 |
| `DescribeUserPermission` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--user-id` | string | ✓ | RAM 用户或 RAM 角色 ID |
| `--all-clusters` | boolean |  | 使用 CleanUserPermissions 清理所有集群权限 |
| `--cluster` | string |  | ACK 集群 ID |
| `--force` | boolean |  | 强制清理 KubeConfig，默认 false（默认：`false`） |

## get

```bash
ecctl ack permission get [flags]
```

查询 RAM 用户/角色的 ACK 权限详情

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `DescribeUserPermission` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--user-id` | string | ✓ | RAM 用户或 RAM 角色 ID |
| `--fields` | string |  | 要包含的资源字段，使用逗号分隔 |

## list

```bash
ecctl ack permission list [flags]
```

列出 RAM 用户/角色的 ACK 权限

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `DescribeUserPermission` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--user-id` | string | ✓ | RAM 用户或 RAM 角色 ID |
| `--fields` | string |  | 要包含的资源字段，使用逗号分隔 |

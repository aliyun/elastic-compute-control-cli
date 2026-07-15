---
title: ack kubeconfig
sidebar_label: kubeconfig
description: "管理 ACK KubeConfig 凭证"
---

# ack kubeconfig

管理 ACK KubeConfig 凭证

运行 `ecctl ack kubeconfig <action> -h` 查看用法，或 `ecctl schema ack.kubeconfig.<action> --full` 获取该命令完整的结构化规格——每个参数与行为，便于 Agent 读取和填参。

## create

```bash
ecctl ack kubeconfig create [flags]
```

签发 KubeConfig

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `DescribeClusterUserKubeconfig` | 未指定 `--user-id` 时 | 读取资源视图。 |
| `DescribeSubaccountK8sClusterUserConfig` | 指定 `--user-id` 时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--cluster` | string | ✓ | ACK 集群 ID |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--expire-time` | integer |  | KubeConfig 有效期；create 按分钟发送，update 按 ACK 要求按小时发送 |
| `--private-ip` | boolean |  | 返回内网访问 KubeConfig |
| `--user-id` | string |  | 用于代签发 KubeConfig 或用户维度状态查询的 RAM 用户或角色 ID |

## update

```bash
ecctl ack kubeconfig update [flags]
```

更新 KubeConfig 过期时间

- 类型：`mutation` · 风险：medium

| API | 调用时机 | 用途 |
|---|---|---|
| `UpdateK8sClusterUserConfigExpire` | 每次执行命令时 | 执行资源操作。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--cluster` | string | ✓ | ACK 集群 ID |
| `--expire-time` | integer | ✓ | KubeConfig 有效期；create 按分钟发送，update 按 ACK 要求按小时发送 |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--user-id` | string | ✓ | 用于代签发 KubeConfig 或用户维度状态查询的 RAM 用户或角色 ID |

## get

```bash
ecctl ack kubeconfig get [flags]
```

获取 KubeConfig

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `DescribeClusterUserKubeconfig` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--cluster` | string | ✓ | ACK 集群 ID |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | 要包含的资源字段，使用逗号分隔 |
| `--private-ip` | boolean |  | 返回内网访问 KubeConfig |

## list

```bash
ecctl ack kubeconfig list [flags]
```

列出 KubeConfig 状态

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `ListClusterKubeconfigStates` | `--scope` 不等于 `user` 时 | 读取资源视图。 |
| `ListUserKubeConfigStates` | `--scope` 等于 `user` 时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--cluster` | string |  | ACK 集群 ID |
| `--fields` | string |  | 要包含的资源字段，使用逗号分隔 |
| `--limit` | integer |  | 最多返回 KubeConfig 状态数量（默认：`50`） |
| `--page` | integer |  | 返回结果页码（默认：`1`） |
| `--scope` | string |  | 状态查询维度（默认：`cluster`） |
| `--user-id` | string |  | 用于代签发 KubeConfig 或用户维度状态查询的 RAM 用户或角色 ID |

## revoke

```bash
ecctl ack kubeconfig revoke [flags]
```

吊销 KubeConfig

- 类型：`mutation` · 风险：medium

| API | 调用时机 | 用途 |
|---|---|---|
| `RevokeK8sClusterKubeConfig` | 每次执行命令时 | 执行资源操作。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--cluster` | string | ✓ | ACK 集群 ID |
| `--region` | string | ✓ | Alibaba Cloud region |

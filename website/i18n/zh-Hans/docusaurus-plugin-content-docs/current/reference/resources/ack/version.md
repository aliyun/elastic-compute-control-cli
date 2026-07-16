---
title: ack version
sidebar_label: version
description: "查询 ACK Kubernetes 版本元数据"
---

# ack version

查询 ACK Kubernetes 版本元数据

运行 `ecctl ack version <action> -h` 查看用法，或 `ecctl schema ack.version.<action> --full` 获取该命令完整的结构化规格——每个参数与行为，便于 Agent 读取和填参。

## list

```bash
ecctl ack version list [flags]
```

列出 Kubernetes 版本元数据

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `DescribeKubernetesVersionMetadata` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--cluster-type` | string |  | 要查询的 ACK 集群类型 |
| `--fields` | string |  | 要包含的资源字段，使用逗号分隔 |
| `--filter` | key_value |  | 过滤表达式 key=value |
| `--kubernetes-version` | string |  | 要查询的 Kubernetes 版本 |
| `--mode` | string |  | 版本查询模式 |
| `--query-upgradable-version` | boolean |  | 指定 Kubernetes 版本时查询可升级版本 |
| `--runtime` | string |  | 用于过滤支持操作系统镜像的容器运行时 |
| `--scenario` | string |  | ACK 集群场景类型 |

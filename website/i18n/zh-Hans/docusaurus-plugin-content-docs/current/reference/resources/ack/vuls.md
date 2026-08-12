---
generated: true
generated_by: "website/scripts/gen-reference.mjs"
generated_command: "make build && npm --prefix website run gen:reference"
title: ack vuls
sidebar_label: vuls
description: "管理 ACK 漏洞扫描和漏洞视图"
---

# ack vuls

管理 ACK 漏洞扫描和漏洞视图

运行 `ecctl ack vuls <action> -h` 查看用法，或 `ecctl schema ack.vuls.<action> --full` 获取该命令完整的结构化规格——每个参数与行为，便于 Agent 读取和填参。

## create

```bash
ecctl ack vuls create [flags]
```

创建一次 ACK 漏洞扫描任务

- 类型：`mutation` · 风险：medium

| API | 调用时机 | 用途 |
|---|---|---|
| `ScanClusterVuls` | 每次执行命令时 | 执行资源操作。 |
| `DescribeClusterVuls` | 未指定 `--no-wait` 时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--cluster` | string | ✓ | ACK 集群 ID |
| `--region` | string | ✓ | Alibaba Cloud region |

## list

```bash
ecctl ack vuls list [flags]
```

列出 ACK 集群或节点池漏洞

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `DescribeClusterVuls` | 未指定 `--nodepool` 时 | 读取资源视图。 |
| `DescribeNodePoolVuls` | 指定 `--nodepool` 时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--cluster` | string | ✓ | ACK 集群 ID |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | 要包含的资源字段，使用逗号分隔 |
| `--nodepool` | string |  | 节点池粒度漏洞视图的节点池 ID |
| `--severity` | string |  | 节点池漏洞修复必要性等级 |

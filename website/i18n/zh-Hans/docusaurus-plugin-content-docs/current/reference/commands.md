---
title: 命令参考
description: 命令发现入口和常见命令形态。
---

# 命令参考

`schema` 和 `--help` 是权威命令参考。本文列出不需要调用云 API 的入口。

## 根命令和版本

```bash
ecctl --help
ecctl --version
```

## 配置

```bash
ecctl configure --help
ecctl configure list
```

读取或切换 profile 的命令要求已存在可用 profile。

## 更新

```bash
ecctl update --check
ecctl update
ecctl update <version>
ecctl update --force
```

Homebrew 支持、指定版本和自动版本检测见[更新](../user-guide/updates.md)。

## Schema

```bash
ecctl schema --help
ecctl schema --list
ecctl schema --list ecs
ecctl schema ecs.instance.create --brief
ecctl schema vpc.vpc.create vpc.vswitch.create ecs.sg.create
```

## 产品帮助

```bash
ecctl vpc create --help
ecctl vpc vswitch create --help
ecctl ecs instance list --help
ecctl ack cluster list --help
ecctl lingjun cluster list --help
```

这些 help 命令可在不调用阿里云 API 的情况下查看 CLI 可用性、必填 flags、可过滤字段和匹配的 schema 名称。

## OpenAPI 元数据

```bash
ecctl call --list --filter ecs --limit 3
ecctl call --schema ecs DescribeInstances --generate-request
```

## 云操作形态

以下形态来自 schema/help 输出。执行前需要替换为你自己的地域、凭证、资源 ID 和产品参数：

| 任务 | 命令形态 |
|---|---|
| 列出 ECS 实例 | `ecctl ecs instance list --region cn-hangzhou` |
| 查看单个 ECS 实例 | `ecctl ecs instance get <instance-id> --region cn-hangzhou` |
| 列出 VPC | `ecctl vpc list --region cn-hangzhou` |
| 创建 vSwitch | `ecctl vpc vswitch create --vpc <vpc-id> --zone <zone-id> --cidr <cidr> --region cn-hangzhou` |
| 列出 ACK 集群 | `ecctl ack list --region cn-hangzhou` |
| 列出灵骏集群 | `ecctl lingjun cluster list --region cn-wulanchabu` |

运行 mutation 命令前先检查对应 schema。

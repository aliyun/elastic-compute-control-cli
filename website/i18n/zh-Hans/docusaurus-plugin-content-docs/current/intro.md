---
sidebar_position: 1
title: 概览
description: ecctl 是什么、覆盖什么，以及从哪里开始。
---

# 概览

`ecctl` 是一个面向阿里云计算、容器和网络资源的 Agent-first 命令行控制器。它为资源操作提供一致的命令形态：

```bash
ecctl <product> <resource> <action> [args] [flags]
```

每个已建模命令都提供结构化的命令规格——必填参数、风险等级、dry-run、幂等和 waiter——Agent 或脚本可在执行前读取。输出默认 JSON，错误结构化，因此 Agent 和脚本以同样的方式读取结果与失败。该模型见 [核心概念](./user-guide/concepts.md)。

当前公开命令面覆盖 ACK、ECS、VPC 和灵骏资源。执行云操作前，先使用 `schema` 查看命令契约：

```bash
ecctl schema --list
ecctl schema --list ecs
ecctl schema ecs.instance.create --brief
```

这些是本地发现命令，不会调用云 API。

## 面向读者

本文档假设你已经理解阿里云产品概念，例如地域、AccessKey、VPC、vSwitch、ECS 实例、安全组和 ACK 集群；也假设你熟悉带本地 profile 的 CLI 使用方式。

`ecctl` 可以读取自己的配置，也可以读取兼容的本地 `aliyun` CLI 配置文件。这只是配置兼容；两个工具提供不同命令面，可以并行使用。

## 阅读顺序

先阅读：

- [安装](./getting-started/installation.md)：构建并验证 CLI。
- [配置](./getting-started/configuration.md)：设置地域、profile、语言和输出默认值。
- [快速开始](./getting-started/quick-start.md)：发现产品并读取一个命令契约。

然后阅读：

- [核心概念](./user-guide/concepts.md)：理解 Agent-first 模型——命令契约、同步执行和结构化输出。
- [命令模型](./user-guide/command-model.md)：理解 product/resource/action 语法。
- [Schema](./user-guide/discovery.md)：使用 `schema` 和 `capabilities`。
- [资源操作](./user-guide/resource-operations.md)：用真实输出走一遍 创建/查看/列举/删除。
- [输出](./user-guide/output.md)：了解 JSON、text、语言和错误结构。
- [OpenAPI 调用](./user-guide/openapi-call.md)：处理尚未建模成资源命令的操作。
- [资源覆盖](./reference/resource-coverage.md)：查看从当前 schema 得到的公开资源列表。

## 事实来源

面向用户的命令契约由资源 specs 生成，并通过 `ecctl schema` 暴露。文档中关于资源参数、waiter、别名和支持动作的描述，均来自本地 `schema` 或 `--help` 输出，而不是手写推测。

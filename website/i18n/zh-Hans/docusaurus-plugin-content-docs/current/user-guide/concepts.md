---
title: 核心概念
description: ecctl 的资源模型和命令契约。
---

# 核心概念

`ecctl` 是面向 Agent 的阿里云资源控制器。它通过统一的资源命令和机器可读契约描述操作，因此 Agent 或脚本可以先检查命令，再变更云资源。

## 资源意图

资源命令使用统一语法，只有嵌套资源包含父资源段：

```bash
ecctl <product> [<parent>] <resource> <action> [id] [flags]
```

`action` 表达用户意图。例如，`ecctl ecs instance update` 可以根据输入字段调用不同的 ECS API，不需要为每个 API 分别暴露顶层命令。产品、资源、动作和别名见[命令模型](./command-model.md)。

## 可检查的命令契约

每个已建模操作都有本地可读的契约：

```bash
ecctl schema ecs.instance.create --brief
```

契约描述必填参数、风险等级、DryRun、幂等、等待行为和输出。具体命令应以契约为准，推荐的检查流程见 [Schema](./discovery.md)。

## 同步资源操作

许多阿里云变更 API 会在资源到达目标状态前返回。ecctl 可以等待目标状态，并在返回前回读资源。需要改变执行时机时，命令契约会提供 `--no-wait` 和 `--timeout`。

[资源操作](./resource-operations.md)通过实际命令输出说明完整生命周期。

## 结构化结果

默认输出为 JSON。结果使用资源语义字段，错误使用稳定对象和非零退出码。工作流执行过的 API 会记录在 `actions` 中，服务端返回 Request ID 时也会保留。

输出模式和错误处理见[输出、语言与错误](./output.md)。

## Spec 驱动行为

资源行为声明在 YAML spec 中。spec 定义参数、OpenAPI 绑定、响应映射、waiter 和命令工作流，CLI 命令面与 `schema` 输出由同一份定义生成。

贡献者可通过[资源 Specs](../contributing/resource-specs.md)了解格式。需要比较 ecctl、直接 OpenAPI 和阿里云 CLI 时，请阅读[通用差异](./common-differences.md)。

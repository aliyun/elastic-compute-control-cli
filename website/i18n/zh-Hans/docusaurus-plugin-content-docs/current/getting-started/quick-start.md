---
title: 快速开始
description: 发现资源，并在执行前检查命令参数和执行行为。
---

# 快速开始

先用本地命令发现能力，再参考下面的常用资源操作。执行前请替换尖括号中的值。
部分示例会修改云资源，应先检查命令 schema 和当前账号环境。

## 构建并检查 CLI

```bash
make build
./bin/ecctl --help
```

帮助输出会列出公开云产品命令和辅助命令。

## 配置默认值

设置默认地域和输出格式：

```bash
ecctl configure set region cn-hangzhou
ecctl configure set output json
```

通过 `ecctl configure set` 设置准备用于云操作的 AccessKey 或 STS 凭证。详见 [配置](./configuration.md)。

## 常用命令

这些命令覆盖运维人员和 Agent 的常见工作流，也集中体现 ecctl 与以 OpenAPI
操作为中心的 aliyun CLI 的区别：资源化动词、机器可读的命令说明、统一过滤、
简化输入、跨 API 工作流、内置等待和结果回读。

### 执行前检查多个命令

```bash
ecctl schema ecs.instance.list ecs.instance.create ecs.disk.create --brief
```

一次返回必填参数、风险、dry-run、幂等和等待行为。

### 查找运行中的生产实例

```bash
ecctl ecs instance list --region cn-hangzhou --filter status=Running --filter tag.env=prod
```

使用统一的过滤语法，返回去除 OpenAPI 包装层后的规范化 JSON。

### 校验 ECS 创建请求

```bash
ecctl ecs instance create --region cn-hangzhou --type <instance-type> --image <image-id-or-name> --sg <sg-id> --vswitch <vswitch-id> --tag env=prod --dry-run
```

使用简短的资源字段，并发送服务端校验请求而不创建实例。

### 创建 ECS 实例

```bash
ecctl ecs instance create --region cn-hangzhou --type <instance-type> --image <image-id-or-name> --sg <sg-id> --vswitch <vswitch-id> --name web-01 --tag env=prod
```

自动提供兼容 ClientToken 的幂等键，等待 `Running` 后回读实例。

### 更新实例并添加标签

```bash
ecctl ecs instance update <instance-id> --region cn-hangzhou --name web-02 --tag env=prod
```

根据资源变更自动选择所需 OpenAPI，最后回读实例。

### 创建云盘

```bash
ecctl ecs disk create --region cn-hangzhou --zone <zone-id> --size 100 --category cloud_essd --name data-01 --tag env=prod
```

自动提供幂等键，等待 `Available` 后返回规范化的云盘视图。

### 绑定密钥对

```bash
ecctl ecs instance update <instance-id> --region cn-hangzhou --key-pair <key-pair-name>
```

资源更新会映射到对应的密钥对 API，并在完成后回读实例。

### 在实例上执行命令

```bash
ecctl ecs instance exec <instance-id> --region cn-hangzhou --command 'uname -a'
```

一条命令完成云助手执行、等待和执行结果回读。

### 放行 HTTPS 安全组规则

```bash
ecctl ecs sg authorize <sg-id> --region cn-hangzhou --rule tcp:443@0.0.0.0/0
```

用紧凑规则替代冗长的 OpenAPI 字段，并回读安全组。

### 获取 ACK kubeconfig

```bash
ecctl ack kubeconfig get --region cn-hangzhou --cluster <cluster-id> --private-ip
```

直接表达资源意图，不要求调用者记住 OpenAPI 操作名和响应结构。

## 列出产品

```bash
ecctl schema --list
```

公开产品：

| Product | 用途 |
|---|---|
| `ack` | ACK 集群及其生命周期操作，包括访问凭证、组件、检查、策略和任务 |
| `agentrun` | AgentRun 沙箱模板和隔离沙箱实例 |
| `ecs` | ECS 实例、云盘、镜像、安全组、ENI、密钥对、启动模板、快照和云助手资源 |
| `lingjun` | 灵骏集群、节点组和高性能网络资源 |
| `rg` | 资源组及治理设置、策略、角色和通知 |
| `tag` | 跨产品标签、关联资源标签规则和标签策略 |
| `vpc` | VPC 和 vSwitch |

## 列出某个产品的命令面

```bash
ecctl schema --list ecs
```

响应会列出 ECS 资源，例如 `instance`、`disk`、`sg`、`image`、`eni`、`keypair`、`launch-template`、`snapshot`、`region` 和 `zone`，以及各自支持的动作。

## 查看命令参数和执行行为

执行 mutation 命令前，先查看 schema：

```bash
ecctl schema ecs.instance.create --brief
```

该命令的输出包含必填参数 `--region`、`--type`、`--image`、`--sg` 和 `--vswitch`，并报告：

- 风险等级 `medium`
- 通过 `--dry-run` 支持 dry-run
- 通过 `ClientToken` 支持幂等
- waiter `running_after_create`
- 默认等待超时 `300s`

## 查看命令帮助

给任意命令加 `-h`（或 `--help`），即可看到这条命令怎么传参：

```bash
ecctl vpc vswitch create --help
```

帮助会把 `--vpc`、`--zone` 和 `--cidr` 标记为必填。

## 直接调用 OpenAPI

当没有资源命令能满足你的需求时，用 `ecctl call` 直接调用阿里云 OpenAPI：先找到操作、生成请求模板、填好后执行调用：

```bash
ecctl call --list --filter ecs
ecctl call --schema ecs DescribeInstances --generate-request
ecctl call ecs DescribeInstances --region cn-hangzhou --request '{"PageSize":10}'
```

详见 [OpenAPI 调用](../user-guide/openapi-call.md)。

## 下一步

- [核心概念](../user-guide/concepts.md) 解释这些命令背后的 Agent-first 模型。
- [资源操作](../user-guide/resource-operations.md) 用真实输出带一个资源走过
  创建、查看、列举和删除。

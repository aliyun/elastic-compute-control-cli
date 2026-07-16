---
title: 快速开始
description: 发现资源，并在执行前检查命令契约。
---

# 快速开始

本快速开始只使用本地发现命令，不创建、更新或删除云资源。

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

## 列出产品

```bash
ecctl schema --list
```

公开产品：

| Product | 用途 |
|---|---|
| `ack` | ACK 集群和部分集群操作 |
| `ecs` | ECS 实例、云盘、镜像、安全组、ENI、密钥对、启动模板、快照和云助手资源 |
| `lingjun` | 灵骏集群和 VPD 网段 |
| `vpc` | VPC 和 vSwitch |

## 列出某个产品的命令面

```bash
ecctl schema --list ecs
```

响应会列出 ECS 资源，例如 `instance`、`disk`、`sg`、`image`、`eni`、`keypair`、`launch-template`、`snapshot`、`region` 和 `zone`，以及各自支持的动作。

## 检查命令契约

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

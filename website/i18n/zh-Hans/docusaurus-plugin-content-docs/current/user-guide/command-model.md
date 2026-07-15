---
title: 命令模型
description: ecctl 如何组织产品、资源、动作、别名和 flags。
---

# 命令模型

资源命令遵循以下形态：

```bash
ecctl <product> <resource> <action> [args] [flags]
```

部分资源位于父资源之下：

```bash
ecctl <product> <parent> <resource> <action> [args] [flags]
```

命令面由 specs 生成，并可通过 `schema` 或 `--help` 检查。

## 产品和资源

列出产品：

```bash
ecctl schema --list
```

列出某个产品的资源和动作：

```bash
ecctl schema --list vpc
ecctl schema --list ack
ecctl schema --list lingjun
```

每个资源条目都包含规范的 `schema_id`。对于嵌套资源，资源 Schema ID 和每个命令 Schema ID 都包含完整的父资源路径：`<product>.<parent>.<resource>[.<action>]`。不接受省略父资源的短写形式。

## 默认资源

部分产品有默认资源。

VPC 有默认 `vpc` 资源：

```bash
ecctl vpc vpc list --help
```

规范用法是 `ecctl vpc vpc list`，帮助中的示例使用短写 `ecctl vpc list`。

ACK 集群操作也可以使用产品级短写：

```bash
ecctl ack list --help
ecctl ack cluster list --help
```

这两个命令都描述 ACK 集群列表。`schema` 查询同时接受规范资源名和显式的 `cluster` 别名：

```bash
ecctl schema ack.ack.create --brief
ecctl schema ack.cluster.create --brief
```

## 资源别名

公开 CLI 接受部分短别名，但帮助中显示规范写法：

| 别名命令 | 帮助显示的规范写法 |
|---|---|
| `ecctl ack kc get --help` | `ecctl ack kubeconfig get` |
| `ecctl ack np list --help` | `ecctl ack nodepool list` |

除非已有脚本必须使用别名，否则文档和自动化中建议使用规范名称。

## Flags

每个资源动作都有全局 flags 和动作专属 flags。命令帮助会标记必填资源 flags：

```bash
ecctl vpc vswitch create --help
```

结构化形式：

```bash
ecctl schema vpc.vswitch.create --brief
```

调用方需要完整 schema 可见参数时使用 `--full`：

```bash
ecctl schema vpc.vswitch.create --full
```

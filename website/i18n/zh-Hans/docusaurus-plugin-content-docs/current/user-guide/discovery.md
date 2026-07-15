---
title: Schema
description: 用 schema 和 capabilities 在执行命令前先查看命令面。
---

# Schema

执行资源操作前，用 `schema` 和 `capabilities` 查看命令面。

## 产品

```bash
ecctl schema --list
```

输出形态：

```json
{
  "products": [
    {"name": "ack"},
    {"name": "ecs"},
    {"name": "lingjun"},
    {"name": "vpc"}
  ]
}
```

完整输出中包含 descriptions。

## 资源和动作

```bash
ecctl schema --list ecs
```

ECS 命令面当前包含 16 个资源，包括：

| Resource | Actions |
|---|---|
| `instance` | `list`, `get`, `create`, `update`, `delete`, `exec`, `monitor`, `reboot`, `renew`, `sendfile`, `start`, `stop` |
| `disk` | `list`, `get`, `create`, `update`, `delete`, `attach`, `clone`, `detach`, `monitor`, `reinit`, `reset` |
| `sg` | `list`, `get`, `create`, `update`, `delete`, `authorize`, `revoke` |
| `image` | `list`, `get`, `create`, `update`, `delete`, `copy`, `export`, `import` |

完整公开覆盖见[资源覆盖](../reference/resource-coverage.md)。

## 命令 Schema

简要 schema：

```bash
ecctl schema ecs.instance.create --brief
```

批量 schema 查询：

```bash
ecctl schema vpc.vpc.create vpc.vswitch.create ecs.sg.create
```

该批量命令返回以 schema 名称为 key 的 JSON object。对于 mutation 命令，schema 可包含：

- 必填参数
- 输出 CLI 形态
- 风险等级
- dry-run 支持
- 幂等模式
- waiter 名称、目标状态、轮询命令和超时

## Capabilities

```bash
ecctl capabilities --output json
```

capabilities payload 声明：

- schema version `1`
- 输出模式 `json` 和 `text`
- 结构化错误写入 `stdout`
- 错误字段包括 `kind`、`code`、`message`、`retryable`、`suggestion`、`field` 和 `accepted_values`
- 公开产品/资源/动作覆盖

## 推荐流程

1. 运行 `ecctl schema --list`。
2. 运行 `ecctl schema --list <product>`。
3. 运行 `ecctl schema <product>.[<parent>.]<resource>.<action> --brief`；只有嵌套资源需要包含父资源段。
4. 需要人类可读 flag 描述时，对具体命令使用 `--help`。
5. 执行云操作时显式指定 `--region`，并使用已知 profile。

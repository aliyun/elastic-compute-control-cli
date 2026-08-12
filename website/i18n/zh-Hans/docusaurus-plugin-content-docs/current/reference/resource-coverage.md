---
title: 资源覆盖
description: ecctl 公开暴露的产品、资源和动作。
---

# 资源覆盖

本文反映以下命令列出的公开命令面：

```bash
ecctl schema --list
ecctl capabilities --output json
```

下文按产品列出资源和动作。每个章节开头的命令可用于重新生成对应列表。

## ACK

```bash
ecctl schema --list ack
```

| Resource | Actions |
|---|---|
| `ack` | `list`, `get`, `create`, `update`, `delete`, `upgrade` |
| `addon` | `list`, `get`, `create`, `update`, `delete`, `upgrade` |
| `alert` | `update` |
| `audit` | `get`, `update` |
| `check` | `list`, `get`, `create` |
| `inspect config` | `get`, `update`, `delete` |
| `audit control-plane-log` | `get`, `update` |
| `event` | `list` |
| `inspect` | 分组命令 |
| `policy instance` | `list`, `get`, `create`, `update`, `delete` |
| `kubeconfig` | `list`, `get`, `create`, `revoke` |
| `node` | `list`, `get`, `delete`, `attach` |
| `nodepool` | `list`, `get`, `create`, `update`, `delete`, `attach`, `detach`, `repair`, `upgrade` |
| `permission` | `list`, `get`, `update`, `delete` |
| `policy` | `list`, `get` |
| `region` | `list` |
| `inspect report` | `list`, `get`, `create` |
| `task` | `list`, `get`, `cancel`, `pause`, `resume` |
| `template` | `list`, `get`, `create`, `update`, `delete` |
| `trigger` | `list`, `get`, `create`, `delete` |
| `version` | `list` |
| `vuls` | `list`, `create` |

ACK 集群命令可以使用产品级短写：

```bash
ecctl ack list --help
ecctl ack cluster list --help
```

## ECS

```bash
ecctl schema --list ecs
```

| Resource | Actions |
|---|---|
| `assistant` | `get`, `update`, `install` |
| `auto-snapshot-policy` | `list`, `get`, `create`, `update`, `delete` |
| `command` | `list`, `get`, `create`, `update`, `delete`, `invoke`, `stop` |
| `disk` | `list`, `get`, `create`, `update`, `delete`, `attach`, `clone`, `detach`, `monitor`, `reinit`, `reset` |
| `eni` | `list`, `get`, `create`, `update`, `delete`, `attach`, `detach` |
| `image` | `list`, `get`, `create`, `update`, `delete`, `copy`, `export`, `import` |
| `instance` | `list`, `get`, `create`, `update`, `delete`, `exec`, `monitor`, `reboot`, `renew`, `sendfile`, `start`, `stop` |
| `keypair` | `list`, `get`, `create`, `delete` |
| `launch-template` | `list`, `get`, `create`, `update`, `delete` |
| `port-range-list` | `list`, `get`, `create`, `update`, `delete` |
| `prefix-list` | `list`, `get`, `create`, `update`, `delete` |
| `region` | `list` |
| `sg` | `list`, `get`, `create`, `update`, `delete`, `authorize`, `revoke` |
| `snapshot` | `list`, `get`, `create`, `update`, `delete`, `copy` |
| `snapshot-group` | `list`, `get`, `create`, `update`, `delete` |
| `zone` | `list` |

## 灵骏

```bash
ecctl schema --list lingjun
```

| Resource | Actions |
|---|---|
| `cluster` | `list`, `get`, `create`, `update`, `delete` |
| `eni` | `list`, `get`, `create`, `update`, `delete` |
| `er` | `list`, `get`, `create`, `update`, `delete` |
| `node-group` | `list`, `get`, `create`, `update`, `delete` |
| `subnet` | `list`, `get`, `create`, `update`, `delete` |
| `vpd` | `list`, `get`, `create`, `update`, `delete` |

## 资源组

```bash
ecctl schema --list rg
```

| Resource | Actions |
|---|---|
| `admin-setting` | `get`, `update` |
| `associated-transfer` | `list`, `update`, `disable`, `enable` |
| `group` | `list`, `get`, `create`, `update`, `delete` |
| `notification` | `get`, `disable`, `enable` |
| `policy` | `list`, `get`, `create`, `delete`, `attach`, `detach` |
| `resource` | `list`, `update` |
| `role` | `list`, `get`, `create`, `update`, `delete` |
| `service-linked-role` | `create`, `delete` |
| `policy version` | `list`, `get`, `create`, `update`, `delete` |

## 标签

```bash
ecctl schema --list tag
```

| Resource | Actions |
|---|---|
| `associated-resource-rule` | `list`, `create`, `update`, `delete` |
| `policy` | `list`, `get`, `create`, `update`, `delete`, `attach`, `detach` |
| `resource` | `list`, `apply`, `remove` |

## VPC

```bash
ecctl schema --list vpc
```

| Resource | Actions |
|---|---|
| `vpc` | `list`, `get`, `create`, `update`, `delete` |
| `vswitch` | `list`, `get`, `create`, `update`, `delete` |

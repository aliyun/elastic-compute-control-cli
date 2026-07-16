---
title: Resource Coverage
description: Public products, resources, and actions exposed by ecctl.
---

# Resource Coverage

This page reflects the public command surface listed by:

```bash
ecctl schema --list
ecctl capabilities --output json
```

The sections below list resources and actions by product. Run the command at
the beginning of a section to reproduce the corresponding list.

## ACK

```bash
ecctl schema --list ack
```

| Resource | Actions |
|---|---|
| `ack` | `list`, `get`, `create`, `update`, `delete`, `upgrade` |
| `kubeconfig` | `list`, `get`, `create`, `update`, `revoke` |
| `node` | `list`, `get`, `delete`, `attach` |
| `nodepool` | `list`, `get`, `create`, `update`, `delete`, `attach`, `detach`, `repair`, `upgrade` |
| `permission` | `list`, `get`, `update`, `delete` |
| `region` | `list` |
| `version` | `list` |

ACK cluster commands can use the short product-level form:

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

## Lingjun

```bash
ecctl schema --list lingjun
```

| Resource | Actions |
|---|---|
| `cluster` | `list`, `get`, `create`, `update`, `delete` |
| `vpd` | `list`, `get`, `create`, `update`, `delete` |

## VPC

```bash
ecctl schema --list vpc
```

| Resource | Actions |
|---|---|
| `vpc` | `list`, `get`, `create`, `update`, `delete` |
| `vswitch` | `list`, `get`, `create`, `update`, `delete` |

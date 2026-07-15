---
title: Command Model
description: How ecctl organizes products, resources, actions, aliases, and flags.
---

# Command Model

Resource commands follow this shape:

```bash
ecctl <product> <resource> <action> [args] [flags]
```

Some resources are nested under a parent resource:

```bash
ecctl <product> <parent> <resource> <action> [args] [flags]
```

The command surface is generated from specs and can be inspected with `schema`
or `--help`.

## Products and Resources

List products:

```bash
ecctl schema --list
```

List resources and actions for one product:

```bash
ecctl schema --list vpc
ecctl schema --list ack
ecctl schema --list lingjun
```

Each resource entry includes its canonical `schema_id`. A nested resource keeps
its full parent path in both the schema ID and command schema ID:
`<product>.<parent>.<resource>[.<action>]`. The shortened form that omits the
parent is not accepted.

## Default Resources

Some products expose a default resource.

VPC has a default `vpc` resource:

```bash
ecctl vpc vpc list --help
```

The usage is `ecctl vpc vpc list`, and the examples in help use the
short form `ecctl vpc list`.

ACK cluster operations can also use the product-level short form:

```bash
ecctl ack list --help
ecctl ack cluster list --help
```

Both commands describe ACK cluster listing. Schema lookup accepts both the
canonical resource name and the explicit cluster alias:

```bash
ecctl schema ack.ack.create --brief
ecctl schema ack.cluster.create --brief
```

## Resource Aliases

The public CLI accepts selected short aliases while displaying canonical usage
in help:

| Alias command | Canonical usage shown by help |
|---|---|
| `ecctl ack kc get --help` | `ecctl ack kubeconfig get` |
| `ecctl ack np list --help` | `ecctl ack nodepool list` |

Use canonical names in documentation and automation unless an alias is required
for compatibility with an existing script.

## Flags

Every resource action has global flags and action-specific flags. Required
resource flags are marked in command help:

```bash
ecctl vpc vswitch create --help
```

For the same information in structured form:

```bash
ecctl schema vpc.vswitch.create --brief
```

Use `--full` when a caller needs all schema-visible parameters:

```bash
ecctl schema vpc.vswitch.create --full
```

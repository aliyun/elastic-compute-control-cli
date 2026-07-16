---
title: Schema
description: Use schema and capabilities to inspect commands before running them.
---

# Schema

Use `schema` and `capabilities` to inspect the command surface before running a
resource operation.

## Products

```bash
ecctl schema --list
```

Output shape:

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

Descriptions are included in the full output.

## Resources and Actions

```bash
ecctl schema --list ecs
```

The ECS surface currently contains 16 resources, including:

| Resource | Actions |
|---|---|
| `instance` | `list`, `get`, `create`, `update`, `delete`, `exec`, `monitor`, `reboot`, `renew`, `sendfile`, `start`, `stop` |
| `disk` | `list`, `get`, `create`, `update`, `delete`, `attach`, `clone`, `detach`, `monitor`, `reinit`, `reset` |
| `sg` | `list`, `get`, `create`, `update`, `delete`, `authorize`, `revoke` |
| `image` | `list`, `get`, `create`, `update`, `delete`, `copy`, `export`, `import` |

Use [Resource Coverage](../reference/resource-coverage.md) for the full public
coverage list.

## Command Schema

Brief schema:

```bash
ecctl schema ecs.instance.create --brief
```

Batch schema lookup:

```bash
ecctl schema vpc.vpc.create vpc.vswitch.create ecs.sg.create
```

The batch command returns a JSON object keyed by schema
name. For mutating commands, the schema can include:

- required parameters
- output CLI form
- risk level
- dry-run support
- idempotency mode
- waiter name, target state, polling command, and timeout

## Capabilities

```bash
ecctl capabilities --output json
```

The capabilities payload declares:

- schema version `1`
- output modes `json` and `text`
- structured errors written to `stdout`
- error fields such as `kind`, `code`, `message`, `retryable`, `suggestion`,
  `field`, and `accepted_values`
- public product/resource/action coverage

## Recommended Flow

1. Run `ecctl schema --list`.
2. Run `ecctl schema --list <product>`.
3. Run `ecctl schema <product>.[<parent>.]<resource>.<action> --brief`; include
   the parent segment only for a nested resource.
4. Use `--help` on the concrete command when you need human-readable flag
   descriptions.
5. Run the cloud operation with explicit `--region` and a known profile.

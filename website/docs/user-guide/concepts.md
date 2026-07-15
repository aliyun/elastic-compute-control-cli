---
title: Concepts
description: The resource model and command contracts behind ecctl.
---

# Concepts

`ecctl` is an Agent-first controller for Alibaba Cloud resources. It presents
resource operations through a regular command grammar and a machine-readable
contract, so an agent or script can inspect a command before it changes cloud
resources.

## Resource intent

Resource commands use a consistent shape, with a parent segment only for
nested resources:

```bash
ecctl <product> [<parent>] <resource> <action> [id] [flags]
```

The action describes the user's intent. `ecctl ecs instance update`, for
example, can route different fields to different ECS APIs without exposing each
API as a separate top-level command. See [Command Model](./command-model.md) for
products, resources, actions, and aliases.

## Inspectable contracts

Every modeled action has a contract that you can read locally:

```bash
ecctl schema ecs.instance.create --brief
```

The contract describes required parameters, risk, dry-run support,
idempotency, waiting behavior, and output. Use it as the source of truth for a
specific command. The recommended inspection flow is documented in
[Schema](./discovery.md).

## Synchronous resource operations

Many Alibaba Cloud mutation APIs return before a resource reaches its target
state. A modeled ecctl operation can wait for that state and read the resource
back before returning. The same contract exposes `--no-wait` and `--timeout`
when callers need different timing.

The [Resource Operations](./resource-operations.md) guide walks through this
lifecycle with command output.

## Structured results

JSON is the default output. Results use resource-oriented fields, and errors
use a stable object with non-zero exit codes. API calls made during a workflow
are recorded in `actions`, including request IDs when the service returns them.

See [Output, Language, and Errors](./output.md) for output modes and error
handling.

## Spec-driven behavior

Resource behavior is declared in YAML specs. The specs define parameters,
OpenAPI bindings, response mapping, waiters, and command workflows. The CLI
surface and `schema` output are generated from the same definitions.

Contributors can read [Resource Specs](../contributing/resource-specs.md) for
the schema format. Users who want to compare this model with direct OpenAPI or
Alibaba Cloud CLI calls should start with
[Common Differences](./common-differences.md).

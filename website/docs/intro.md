---
sidebar_position: 1
title: Overview
description: What ecctl is, what it covers, and where to start.
---

# Overview

`ecctl` is an Agent-first command-line controller for Alibaba Cloud compute,
container, and network resources. It exposes a consistent command shape for
resource operations:

```bash
ecctl <product> <resource> <action> [args] [flags]
```

Every modeled command exposes a structured spec — required parameters, risk
level, dry-run, idempotency, and waiters — that an agent or script can read
before execution. Output is JSON-first and errors are structured, so agents and scripts
read results and failures the same way. See [Concepts](./user-guide/concepts.md)
for this model.

The current public command surface covers ACK, ECS, VPC, and Lingjun resources.
Use `schema` before running a cloud operation:

```bash
ecctl schema --list
ecctl schema --list ecs
ecctl schema ecs.instance.create --brief
```

These are local discovery commands; they do not call cloud APIs.

## Audience

This documentation assumes that you already understand Alibaba Cloud product
concepts such as regions, AccessKeys, VPCs, vSwitches, ECS instances, security
groups, and ACK clusters. It also assumes you are comfortable using a CLI with
local profiles.

`ecctl` can read its own configuration and compatible local `aliyun` CLI
configuration files. This is configuration compatibility only; the two tools
serve different command surfaces and can be used side by side.

## What to Read

Start with:

- [Installation](./getting-started/installation.md) to build and verify the CLI.
- [Configuration](./getting-started/configuration.md) to set region, profile,
  language, and output defaults.
- [Quick Start](./getting-started/quick-start.md) to discover products and read a
  command contract.

Then use:

- [Concepts](./user-guide/concepts.md) for the Agent-first model: command
  contracts, synchronous execution, and structured output.
- [Command Model](./user-guide/command-model.md) for product/resource/action
  grammar.
- [Schema](./user-guide/discovery.md) for `schema` and `capabilities`.
- [Resource Operations](./user-guide/resource-operations.md) for a full
  create/inspect/list/delete walkthrough with real output.
- [Output](./user-guide/output.md) for JSON, text, language, and errors.
- [OpenAPI Calls](./user-guide/openapi-call.md) when a resource command is not
  modeled yet.
- [Resource Coverage](./reference/resource-coverage.md) for the public resource
  list generated from the current schema.

## Source of Truth

The user-facing command contract is generated from resource specs and exposed by
`ecctl schema`. When a page shows resource parameters, waiter behavior, aliases,
or supported actions, it is based on local `schema` or `--help` output rather
than hand-written assumptions.

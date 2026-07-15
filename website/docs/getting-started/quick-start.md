---
title: Quick Start
description: Discover resources and inspect a command before running it.
---

# Quick Start

This quick start uses local discovery commands. It does not create, update, or
delete cloud resources.

## Build and Check the CLI

```bash
make build
./bin/ecctl --help
```

The help output lists public cloud product commands and auxiliary commands.

## Configure Defaults

Set a default region and output format:

```bash
ecctl configure set region cn-hangzhou
ecctl configure set output json
```

Set the AccessKey or STS credentials you intend to use for cloud operations with
`ecctl configure set`. See [Configuration](./configuration.md) for details.

## List Products

```bash
ecctl schema --list
```

Public products:

| Product | Purpose |
|---|---|
| `ack` | ACK clusters and selected cluster operations |
| `ecs` | ECS instances, disks, images, security groups, ENIs, key pairs, launch templates, snapshots, and Cloud Assistant resources |
| `lingjun` | Lingjun clusters and VPD network segments |
| `vpc` | VPCs and vSwitches |

## List a Product Surface

```bash
ecctl schema --list ecs
```

The response lists ECS resources such as `instance`, `disk`, `sg`, `image`,
`eni`, `keypair`, `launch-template`, `snapshot`, `region`, and `zone`, each with
its supported actions.

## Inspect a Command Contract

Before running a mutating command, inspect its schema:

```bash
ecctl schema ecs.instance.create --brief
```

The output for this command includes required parameters `--region`,
`--type`, `--image`, `--sg`, and `--vswitch`. It also reports:

- risk level `medium`
- dry-run support through `--dry-run`
- idempotency through `ClientToken`
- waiter `running_after_create`
- default wait timeout `300s`

## Read a Command's Help

Add `-h` (or `--help`) to any command to see how to pass its parameters:

```bash
ecctl vpc vswitch create --help
```

The help marks `--vpc`, `--zone`, and `--cidr` as required.

## Call the OpenAPI Directly

When no resource command covers what you need, call the Alibaba Cloud OpenAPI
directly with `ecctl call`. Find the operation, generate a request template, fill
it in, and run the call:

```bash
ecctl call --list --filter ecs
ecctl call --schema ecs DescribeInstances --generate-request
ecctl call ecs DescribeInstances --region cn-hangzhou --request '{"PageSize":10}'
```

See [OpenAPI Calls](../user-guide/openapi-call.md) for details.

## Next Steps

- [Concepts](../user-guide/concepts.md) explains the Agent-first model behind
  these commands.
- [Resource Operations](../user-guide/resource-operations.md) walks a resource
  through create, inspect, list, and delete with real output.

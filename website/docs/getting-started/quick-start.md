---
title: Quick Start
description: Discover resources and inspect a command before running it.
---

# Quick Start

Start with the local discovery commands, then use the common resource workflows
below. Replace values in angle brackets before running a command. Some examples
mutate cloud resources; inspect their schema and account context first.

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

Configure a supported Alibaba Cloud credential profile, then select it with
`--profile` or the profile environment variable. Local AK and STS credentials
may also be set with `ecctl configure set`. See [Configuration](./configuration.md)
for details.

## Common Commands

These commands are curated for common operator and Agent workflows. Together
they show where ecctl differs from Alibaba Cloud CLI's API-operation-oriented
interface: resource verbs, machine-readable command details, normalized
filters, concise inputs, multi-API workflows, built-in waiters, and readback.

### Check several commands before use

```bash
ecctl schema ecs.instance.list ecs.instance.create ecs.disk.create --brief
```

One call returns required flags, risk, dry-run, idempotency, and waiter behavior.

### Find running production instances

```bash
ecctl ecs instance list --region cn-hangzhou --filter status=Running --filter tag.env=prod
```

The consistent filter syntax returns normalized JSON without OpenAPI response wrappers.

### Validate ECS creation

```bash
ecctl ecs instance create --region cn-hangzhou --type <instance-type> --image <image-id-or-name> --sg <sg-id> --vswitch <vswitch-id> --tag env=prod --dry-run
```

Short resource fields send a server-side validation request without creating an instance.

### Create an ECS instance

```bash
ecctl ecs instance create --region cn-hangzhou --type <instance-type> --image <image-id-or-name> --sg <sg-id> --vswitch <vswitch-id> --name web-01 --tag env=prod
```

ecctl supplies ClientToken-compatible idempotency, waits for `Running`, and reads the instance back.

### Update and tag an instance

```bash
ecctl ecs instance update <instance-id> --region cn-hangzhou --name web-02 --tag env=prod
```

ecctl selects the required OpenAPIs from the requested resource changes and reads the instance back.

### Create a cloud disk

```bash
ecctl ecs disk create --region cn-hangzhou --zone <zone-id> --size 100 --category cloud_essd --name data-01 --tag env=prod
```

The command supplies idempotency, waits for `Available`, and returns the normalized disk view.

### Attach a key pair

```bash
ecctl ecs instance update <instance-id> --region cn-hangzhou --key-pair <key-pair-name>
```

The resource update maps to the required key-pair API and reads the instance back.

### Run a command on an instance

```bash
ecctl ecs instance exec <instance-id> --region cn-hangzhou --command 'uname -a'
```

One command runs Cloud Assistant, waits for completion, and reads the invocation result.

### Authorize an HTTPS security-group rule

```bash
ecctl ecs sg authorize <sg-id> --region cn-hangzhou --rule tcp:443@0.0.0.0/0
```

A compact rule replaces verbose OpenAPI fields, and ecctl reads the security group back.

### Get an ACK kubeconfig

```bash
ecctl ack kubeconfig get --region cn-hangzhou --cluster <cluster-id> --private-ip
```

The resource intent is explicit; callers do not need to know the OpenAPI operation or response shape.

## List Products

```bash
ecctl schema --list
```

Public products:

| Product | Purpose |
|---|---|
| `ack` | ACK clusters and cluster lifecycle operations, including access, addons, checks, policies, and tasks |
| `agentrun` | AgentRun sandbox templates and isolated sandbox instances |
| `ecs` | ECS instances, disks, images, security groups, ENIs, key pairs, launch templates, snapshots, and Cloud Assistant resources |
| `lingjun` | Lingjun clusters, node groups, and high-performance network resources |
| `rg` | Resource groups and governance settings, policies, roles, and notifications |
| `tag` | Cross-product tags, associated-resource tag rules, and tag policies |
| `vpc` | VPCs and vSwitches |

## List a Product Surface

```bash
ecctl schema --list ecs
```

The response lists ECS resources such as `instance`, `disk`, `sg`, `image`,
`eni`, `keypair`, `launch-template`, `snapshot`, `region`, and `zone`, each with
its supported actions.

## Inspect Command Details

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

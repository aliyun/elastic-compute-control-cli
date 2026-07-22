---
title: Command Reference
description: Command discovery entry points and common command forms.
---

# Command Reference

Use `schema` and `--help` as the authoritative command reference. This page
lists entry points that do not require cloud API calls.

## Root and Version

```bash
ecctl --help
ecctl --version
```

## Configuration

```bash
ecctl configure --help
ecctl configure list
```

Commands that read or switch a profile require a configured profile.

## Updates

```bash
ecctl update --check
ecctl update
ecctl update <version>
ecctl update --force
```

See [Updates](../user-guide/updates.md) for Homebrew support, explicit versions,
and automatic version checks.

## Schema

```bash
ecctl schema --help
ecctl schema --list
ecctl schema --list ecs
ecctl schema ecs.instance.create --brief
ecctl schema vpc.vpc.create vpc.vswitch.create ecs.sg.create
```

## Product Help

```bash
ecctl vpc create --help
ecctl vpc vswitch create --help
ecctl ecs instance list --help
ecctl ack cluster list --help
ecctl lingjun cluster list --help
```

These help commands show CLI availability, required flags, filterable fields,
and matching schema names without calling Alibaba Cloud APIs.

## OpenAPI Metadata

```bash
ecctl call --list --filter ecs --limit 3
ecctl call --schema ecs DescribeInstances --generate-request
```

## Cloud Operation Forms

The following forms come from schema/help output. They require your own
region, credentials, resource IDs, and product-specific values before execution:

| Task | Command form |
|---|---|
| List ECS instances | `ecctl ecs instance list --region cn-hangzhou` |
| Inspect one ECS instance | `ecctl ecs instance get <instance-id> --region cn-hangzhou` |
| List VPCs | `ecctl vpc list --region cn-hangzhou` |
| Create a vSwitch | `ecctl vpc vswitch create --vpc <vpc-id> --zone <zone-id> --cidr <cidr> --region cn-hangzhou` |
| List ACK clusters | `ecctl ack list --region cn-hangzhou` |
| List Lingjun clusters | `ecctl lingjun cluster list --region cn-wulanchabu` |

Inspect the matching schema before running a mutating command.

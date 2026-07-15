---
title: Resource-specific Optimizations
description: Index of ecctl behavior that differs from direct OpenAPI calls for specific resources.
---

# Resource-specific Optimizations

The [Common Differences](../common-differences.md) page describes behavior
shared across ecctl resource commands. The pages in this section list
resource-specific conversions and workflows that change how a user invokes an
operation or reads its result.

Each product page is organized by resource. Every difference is described in
prose first, followed by concrete Alibaba Cloud CLI or OpenAPI and ecctl
examples. Conditions and limits stay next to the behavior they qualify.

## ECS

[ECS optimizations](./ecs.md) cover image-name resolution, security-group rule
shorthand, desired-state updates, multi-API routing, optional detail queries,
and decoded Cloud Assistant output.

## ACK

[ACK optimizations](./ack.md) cover cluster and node-pool workflows, optional
detail queries, kubeconfig operations, permission updates, and version metadata
validation.

## VPC

[VPC optimizations](./vpc.md) cover server-side DryRun, idempotency, and
normalized list output for VPCs and vSwitches.

## Lingjun

[Lingjun optimizations](./lingjun.md) cover cluster scaling, optional node
queries, asynchronous VPD creation, and secondary CIDR management.

Each entry is limited to the public command surface reported by:

```bash
ecctl capabilities --output json
ecctl schema --list
```

For exact flags, defaults, and command contracts, use `ecctl schema` or the
[resource reference](../../reference/resource-coverage.md).

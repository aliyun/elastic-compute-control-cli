---
title: Resource Operations
description: Create, inspect, list, and delete resources with synchronous, structured results.
---

# Resource Operations

This page walks one resource through its full lifecycle. The JSON below is
abbreviated to the relevant fields. Resource operations are synchronous by
default — see [Concepts](./concepts.md) for the model.

## Create

A create command returns after the resource reaches its target state, and it
reads the resource back so the response is a final view rather than a pending
operation.

```bash
ecctl vpc create --cidr 192.168.0.0/16 --name demo --region cn-beijing
```

```json
{
  "actions": [
    {"action_name": "CreateVpc", "request_id": "1A2B3C4D-5E6F-7A8B-9C0D-1E2F3A4B5C6D"},
    {"action_name": "DescribeVpcAttribute", "request_id": "2B3C4D5E-6F7A-8B9C-0D1E-2F3A4B5C6D7E"}
  ],
  "ecctl_capabilities_used": ["auto_wait"],
  "vpc": {
    "id": "vpc-2zexxxxxxxxxxxxxxxxx",
    "name": "demo",
    "cidr": "192.168.0.0/16",
    "status": "Available",
    "region": "cn-beijing",
    "creation_time": "2026-06-24T06:02:36Z"
  }
}
```

Three things are visible in every mutation response:

- `actions` lists each Alibaba Cloud API call with its `request_id`. Here the
  create (`CreateVpc`) is followed by the read-back (`DescribeVpcAttribute`).
- `ecctl_capabilities_used` reports `auto_wait`, meaning the command waited for
  the target state before returning.
- The resource object (`vpc`) is the final view, including the assigned `id` and
  a `status` of `Available`.

Creating a vSwitch in that VPC follows the same shape:

```bash
ecctl vpc vswitch create \
  --vpc vpc-2zexxxxxxxxxxxxxxxxx \
  --zone cn-beijing-h --cidr 192.168.1.0/24 \
  --name demo-vsw --region cn-beijing
```

```json
{
  "actions": [
    {"action_name": "CreateVSwitch"},
    {"action_name": "DescribeVSwitchAttributes"}
  ],
  "ecctl_capabilities_used": ["auto_wait"],
  "vswitch": {
    "id": "vsw-2zexxxxxxxxxxxxxxxxx",
    "vpc": "vpc-2zexxxxxxxxxxxxxxxxx",
    "zone": "cn-beijing-h",
    "cidr": "192.168.1.0/24",
    "available_ip_count": 252,
    "status": "Available"
  }
}
```

## Inspect

`get` returns a single resource by ID:

```bash
ecctl vpc get vpc-2zexxxxxxxxxxxxxxxxx --region cn-beijing
```

```json
{
  "vpc": {
    "id": "vpc-2zexxxxxxxxxxxxxxxxx",
    "name": "demo",
    "cidr": "192.168.0.0/16",
    "status": "Available",
    "cloud_resources": [
      {"resource_type": "VSwitch", "resource_count": 1},
      {"resource_type": "VRouter", "resource_count": 1},
      {"resource_type": "RouteTable", "resource_count": 1}
    ]
  }
}
```

## List and Filter

`list` is paginated, and every filter is passed through `--filter key=value`
rather than a per-field flag:

```bash
ecctl vpc vswitch list --filter vpc=vpc-2zexxxxxxxxxxxxxxxxx --region cn-beijing
```

```json
{
  "pagination": {"page": 1, "limit": 50, "returned": 1, "has_more": false},
  "total": 1,
  "vswitches": [
    {
      "id": "vsw-2zexxxxxxxxxxxxxxxxx",
      "vpc": "vpc-2zexxxxxxxxxxxxxxxxx",
      "zone": "cn-beijing-h",
      "cidr": "192.168.1.0/24",
      "status": "Available"
    }
  ]
}
```

The `pagination` block reports the current page, page size, number of returned
items, and whether more pages exist.

## Delete

`delete` is synchronous as well: it waits until the resource is absent and
reports `deleted`. Delete the vSwitch before the VPC.

```bash
ecctl vpc vswitch delete vsw-2zexxxxxxxxxxxxxxxxx --region cn-beijing
ecctl vpc delete vpc-2zexxxxxxxxxxxxxxxxx --region cn-beijing
```

```json
{
  "actions": [
    {"action_name": "DeleteVpc"},
    {"action_name": "DescribeVpcs"}
  ],
  "deleted": true,
  "ecctl_capabilities_used": ["auto_wait"],
  "vpc": {"id": "vpc-2zexxxxxxxxxxxxxxxxx"}
}
```

Destructive deletes accept `--force`. Reading back a deleted resource returns a
structured `not_found` error:

```bash
ecctl vpc get vpc-2zexxxxxxxxxxxxxxxxx --region cn-beijing
```

```json
{
  "error": {
    "kind": "not_found",
    "code": "NotFound",
    "message": "vpc not found",
    "retryable": false
  }
}
```

See [Output, Language, and Errors](./output.md) for the full error model.

## Control the Wait

Waiting behavior is part of the contract. Read it with `schema`:

```bash
ecctl schema vpc.vpc.create --brief
```

The `contract.wait` for `vpc.vpc.create` names the waiter
`available_after_create`, a default `timeout` of `300s`, the opt-out flag
`--no-wait`, and the poll command `ecctl vpc get <id> --region <region> --output json`.

Override per command:

```bash
ecctl vpc create --cidr 192.168.0.0/16 --no-wait --region cn-beijing
ecctl vpc create --cidr 192.168.0.0/16 --timeout 600s --region cn-beijing
```

`--no-wait` returns before the target state is reached. `--timeout` changes the
upper bound on waiting.

## Validate and Stay Idempotent

When the contract reports `dry_run` support, validate a mutation without applying
it:

```bash
ecctl vpc create --cidr 192.168.0.0/16 --dry-run --region cn-beijing
```

Mutations that support idempotency carry a `ClientToken`. Pass an explicit key so
a retried command does not create a duplicate:

```bash
ecctl vpc create --cidr 192.168.0.0/16 --idempotency-key <token> --region cn-beijing
```

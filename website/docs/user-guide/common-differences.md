---
title: Common Differences
description: Compare ecctl resource commands with Alibaba Cloud CLI and direct OpenAPI calls.
---

# Common Differences

`ecctl`, Alibaba Cloud CLI, and direct OpenAPI calls can reach the same cloud
services, but they expose different contracts. Alibaba Cloud CLI follows API
operations and parameters closely. Direct OpenAPI calls add signing, endpoint,
and request serialization. ecctl adds a resource model for the public commands
it supports.

## At a glance

| Area | ecctl resource command | Alibaba Cloud CLI or direct OpenAPI |
|---|---|---|
| Command | Resource and action, such as `ecs instance create` | Product and API operation, such as `ecs RunInstances` |
| Input | Resource fields such as `--type`, `--image`, and `--sg` | OpenAPI fields such as `InstanceType`, `ImageId`, and `SecurityGroupId` |
| Workflow | Can call several APIs, wait, and read the resource back | Usually one API call; generic polling must be configured separately |
| Output | Curated resource view with snake_case fields | API response shape and wrapper fields |
| Errors | Structured error plus recorded API actions | API or CLI error from the current call |
| Discovery | `capabilities` and per-command `schema` | API help and metadata |
| Coverage | Public, modeled resource operations | Broad OpenAPI coverage |

## Resource-oriented commands

ecctl uses `product/[parent/]resource/action` instead of API operation names;
the parent segment appears only for nested resources. IDs are usually
positional, and related API operations share one user action. A single
`ecs instance update` command can change attributes, networking, security
groups, tags, or attached identities by selecting the required APIs from the
fields you pass.

Alibaba Cloud CLI remains the better fit when you need a specific API operation
and its exact request shape. See [Command Model](./command-model.md) for ecctl's
grammar.

For example, Alibaba Cloud CLI uses the `ModifyInstanceAttribute` operation and
OpenAPI parameter names to rename an instance:

```bash
aliyun ecs ModifyInstanceAttribute \
  --region cn-hangzhou \
  --InstanceId i-bp1234567890example \
  --InstanceName web-02
```

The corresponding ecctl command uses the resource, action, positional resource
ID, and resource field name:

```bash
ecctl ecs instance update i-bp1234567890example \
  --region cn-hangzhou \
  --name web-02
```

## Resource-oriented input

ecctl maps OpenAPI names to shorter resource fields and kebab-case flags. For
ECS instance creation, `InstanceType`, `ImageId`, `SecurityGroupId`, and
`VSwitchId` become `--type`, `--image`, `--sg`, and `--vswitch`.

The following commands pass the same resource inputs. The IDs are illustrative;
replace them with resources from your account. Alibaba Cloud CLI keeps the
OpenAPI parameter names and represents a tag as separate key and value fields:

```bash
aliyun ecs RunInstances \
  --RegionId cn-hangzhou \
  --InstanceType ecs.e3.medium \
  --ImageId aliyun_3_x64_20G_alibase_20240528.vhd \
  --SecurityGroupId sg-bp1234567890example \
  --VSwitchId vsw-bp1234567890example \
  --InstanceName web-01 \
  --Tag.1.Key env \
  --Tag.1.Value prod
```

ecctl uses resource fields and accepts the tag as one `key=value` input:

```bash
ecctl ecs instance create \
  --region cn-hangzhou \
  --type ecs.e3.medium \
  --image aliyun_3_x64_20G_alibase_20240528.vhd \
  --sg sg-bp1234567890example \
  --vswitch vsw-bp1234567890example \
  --name web-01 \
  --tag env=prod
```

Filters use `--filter key=value`, and tags use `--tag key=value`. Structured
objects can use inline values, JSON, or `@file.json` where the command schema
allows them. Some resources add input conversions that OpenAPI does not offer,
such as resolving an ECS image name or parsing a security-group rule shorthand.
Those conversions are listed under
[Resource-specific Optimizations](./resource-optimizations/index.md).

## Multi-API workflows and waiting

A resource command can call a mutation API, poll a read API until the resource
reaches its target state, and return the latest resource view. This behavior is
declared per command. Use `--no-wait` to return after the mutation call, or
`--timeout` to change the wait bound.

Alibaba Cloud CLI provides a generic waiter, but the caller supplies the query,
expression, target value, and timing. ecctl's schema already names the probe,
target state, failure states, and timeout for modeled operations.

For example, after `RunInstances` returns an instance ID, an Alibaba Cloud CLI
caller can issue a separate query and configure its waiter:

```bash
aliyun ecs DescribeInstances \
  --RegionId cn-hangzhou \
  --InstanceIds '["i-bp1234567890example"]' \
  --waiter expr='Instances.Instance[0].Status' to=Running
```

ecctl instance creation already polls `DescribeInstances` until the instance is
`Running` and then reads it back. `--timeout` only changes the upper bound:

```bash
ecctl ecs instance create \
  --region cn-hangzhou \
  --type ecs.e3.medium \
  --image aliyun_3_x64_20G_alibase_20240528.vhd \
  --sg sg-bp1234567890example \
  --vswitch vsw-bp1234567890example \
  --timeout 10m
```

## Normalized output and pagination

OpenAPI list responses often use nested wrappers such as
`Instances.Instance[]`. ecctl removes those wrappers, maps selected fields to
snake_case, and returns the resource array with `pagination`. A top-level
`total` is included only when the API returns a meaningful total. Page-number
APIs and token APIs retain their respective pagination models, but use a
consistent ecctl envelope.

For example, the raw list command and its abbreviated response retain the
OpenAPI wrappers and field names:

```bash
aliyun ecs DescribeInstances \
  --RegionId cn-hangzhou \
  --MaxResults 2
```

```json
{
  "Instances": {
    "Instance": [
      {"InstanceId": "i-bp1234567890example", "InstanceName": "web-01", "Status": "Running"},
      {"InstanceId": "i-bp0987654321example", "InstanceName": "web-02", "Status": "Running"}
    ]
  },
  "NextToken": "next-page-token",
  "RequestId": "A1B2C3D4-1111-2222-3333-1234567890AB"
}
```

Because `DescribeInstances` supports token pagination, the ecctl command uses
`--limit` for the first page and returns the resource array without the
`Instances.Instance` wrapper:

```bash
ecctl ecs instance list --region cn-hangzhou --limit 2
```

```json
{
  "instances": [
    {"id": "i-bp1234567890example", "name": "web-01", "status": "Running"},
    {"id": "i-bp0987654321example", "name": "web-02", "status": "Running"}
  ],
  "pagination": {
    "limit": 2,
    "returned": 2,
    "has_more": true,
    "next_token": "next-page-token"
  }
}
```

To fetch the next page, pass `pagination.next_token` to `--next-token`.

Mutation output includes `actions` and the resource view or deletion result.
Each action records the API name and the service request ID when available. See
[Output, Language, and Errors](./output.md) for the output contract.

For example, this command creates an ECS instance and waits until it is
`Running`:

```bash
ecctl ecs instance create \
  --region cn-hangzhou \
  --type ecs.e3.medium \
  --image aliyun_3_x64_20G_alibase_20240528.vhd \
  --sg sg-bp1234567890example \
  --vswitch vsw-bp1234567890example
```

The result records both the `RunInstances` and `DescribeInstances` calls:

```json
{
  "actions": [
    {"action_name": "RunInstances", "request_id": "A1B2C3D4-1111-2222-3333-1234567890AB"},
    {"action_name": "DescribeInstances", "request_id": "B2C3D4E5-2222-3333-4444-1234567890AB"}
  ],
  "instance": {"id": "i-bp1234567890example", "status": "Running"}
}
```

## Dry run, idempotency, and safety

When an OpenAPI supports server-side DryRun, ecctl maps the service's
`DryRunOperation` response to a successful result such as
`{"dry_run":"passed"}`. This differs from a CLI-only request preview, which
does not send the request.

Commands backed by an OpenAPI idempotency field expose `--idempotency-key` and
can generate a compatible token when it is omitted. Destructive commands keep
safe defaults, such as requiring `--force` explicitly when a forced release is
needed.

Alibaba Cloud CLI's global `--dryrun` previews the serialized request without
sending it. Its critical output and process status are:

```bash
aliyun vpc DeleteVpc \
  --RegionId cn-hangzhou \
  --VpcId vpc-bp1234567890example \
  --dryrun
```

```text
Skip invoke in dry-run mode, request is:
------------------------------------
POST /?...&Action=DeleteVpc&...&VpcId=vpc-bp1234567890example&... HTTPS/1.1
Host: vpc.aliyuncs.com
...

Exit code: 0
```

The OpenAPI `DryRun` parameter is different because it sends a validation
request to the service:

```bash
aliyun vpc DeleteVpc \
  --RegionId cn-hangzhou \
  --VpcId vpc-bp1234567890example \
  --DryRun true
```

A passed validation is returned as the `DryRunOperation` error sentinel.
Alibaba Cloud CLI therefore exits with status 1:

```text
ERROR: SDK.ServerError
ErrorCode: DryRunOperation
RequestId: ...
Message: Request validation has been passed with DryRun flag set.
...

Exit code: 1
```

ecctl's `--dry-run` uses the service-side validation and reports a passed check
as a success result:

```bash
ecctl vpc delete vpc-bp1234567890example \
  --region cn-hangzhou \
  --dry-run
```

```json
{
  "actions": [{"action_name": "DeleteVpc", ...}],
  "requested_count": 1,
  "available_count": 1,
  "dry_run": "passed"
}

Exit code: 0
```

For idempotent creation, Alibaba Cloud CLI exposes the raw `ClientToken`, while
ecctl gives it a resource-command name:

Alibaba Cloud CLI:

```bash
aliyun vpc CreateVpc \
  --RegionId cn-hangzhou \
  --VpcName prod-vpc \
  --CidrBlock 10.0.0.0/16 \
  --ClientToken provisioning-42
```

ecctl:

```bash
ecctl vpc create \
  --region cn-hangzhou \
  --name prod-vpc \
  --cidr 10.0.0.0/16 \
  --idempotency-key provisioning-42
```

Forced instance release is explicit in both interfaces. ecctl keeps the safety
choice on the resource action:

Alibaba Cloud CLI:

```bash
aliyun ecs DeleteInstance \
  --region cn-hangzhou \
  --InstanceId i-bp1234567890example \
  --Force true
```

ecctl:

```bash
ecctl ecs instance delete i-bp1234567890example \
  --region cn-hangzhou \
  --force
```

## Structured errors and suggestions

ecctl writes structured success and error payloads to stdout. Error objects
include a stable category, code, retry guidance, and optional field or suggested
action. The `actions` array preserves the original OpenAPI code, message, and
request ID.

Some resource workflows add context-specific guidance. ECS instance creation
can suggest a `DescribeAvailableResource` query for stock errors, and instance
deletion can explain when `--force` or a prior stop is required.

For example, when `RunInstances` reports that the requested instance type is not
available in a zone, ecctl preserves the provider error in `actions` and adds a
field-specific recovery command. The raw OpenAPI error contains only the
provider fields:

```json
{
  "Code": "InvalidResourceType.NotSupported",
  "Message": "instance type ecs.g6.large not exists in [cn-shanghai-g]",
  "RequestId": "..."
}
```

ecctl keeps those fields in `actions` and adds the stable error category,
affected resource field, retry guidance, and recovery command. The abbreviated
result is:

```json
{
  "error": {
    "kind": "service",
    "code": "CloudAPIError",
    "field": "type",
    "retryable": false,
    "suggested_action": "ecctl call ecs DescribeAvailableResource --region cn-shanghai --ZoneId cn-shanghai-g --DestinationResource InstanceType --InstanceType ecs.g6.large"
  },
  "actions": [
    {
      "action_name": "RunInstances",
      "code": "InvalidResourceType.NotSupported",
      "message": "instance type ecs.g6.large not exists in [cn-shanghai-g]",
      "request_id": "..."
    }
  ]
}
```

## Discovery and escape hatches

Alibaba Cloud CLI describes the raw API operation:

```bash
aliyun ecs RunInstances help
```

The help output is API-oriented and lists the OpenAPI parameters:

```text
Parameters:
  --RegionId String  Required
  ...
  --ImageId String  Optional
  --InstanceType String  Optional
  ...
  --SecurityGroupId String  Optional
  ...
  --VSwitchId String  Optional
```

ecctl describes the modeled resource commands and their contracts:

```bash
ecctl capabilities --output json
ecctl schema ecs.instance.create --brief
```

The abbreviated `capabilities` result describes the global machine interface:

```json
{
  "cli": "ecctl",
  "schema_version": 1,
  "output_modes": ["json", "text"],
  "schema": {"supported": true, ...},
  "errors": {"structured": true, "stream": "stdout", ...},
  ...
}
```

The command schema narrows that description to one resource action:

```json
{
  "command": "ecs.instance.create",
  "kind": "mutation",
  "params": {
    "image": {"type": "string", "required": true},
    "type": {"type": "string", "required": true},
    ...
  },
  "contract": {
    "dry_run": {"supported": true, "flag": "dry-run"},
    "idempotency": {"supported": true, "field": "ClientToken", ...},
    "wait": {"waitable": true, "no_wait_flag": "no-wait", ...}
  }
}
```

The schema reports required parameters, risk, DryRun, idempotency, and waiting.
Use `--api-param key=value` only on operations that expose it when you need an
extra request field.

Use [`ecctl call`](./openapi-call.md) for an operation that is not modeled as a
public resource command. `ecctl call` keeps the raw OpenAPI operation and
request shape; it does not add resource waiters, idempotency injection, or
response normalization.

For example, Alibaba Cloud CLI returns the raw OpenAPI response for stock
discovery:

```bash
aliyun ecs DescribeAvailableResource \
  --RegionId cn-hangzhou \
  --DestinationResource InstanceType \
  --IoOptimized optimized \
  --InstanceType ecs.e3.medium
```

```json
{
  "RequestId": "...",
  "AvailableZones": {...}
}
```

The equivalent `ecctl call` keeps that response unchanged under `response` and
adds request metadata around it:

```bash
ecctl call ecs DescribeAvailableResource \
  --region cn-hangzhou \
  --DestinationResource InstanceType \
  --IoOptimized optimized \
  --InstanceType ecs.e3.medium
```

```json
{
  "product": "ecs",
  "operation": "DescribeAvailableResource",
  "region": "cn-hangzhou",
  "response": {
    "RequestId": "...",
    "AvailableZones": {...}
  }
}
```

Only the outer envelope changes. Fields inside `response` retain the original
OpenAPI names and structure; `ecctl call` does not apply resource output
normalization, waiting, or idempotency injection.

## Choose the right interface

Use an ecctl resource command when you want a stable resource contract,
synchronous completion, and normalized output. Use Alibaba Cloud CLI or
`ecctl call` when you need broad API coverage or exact control over one API
request. Use a direct SDK or HTTP request when the application must own request
construction, retries, and integration with its runtime.

For example, use ecctl when provisioning needs one synchronous resource action:

```bash
ecctl ecs instance create \
  --region cn-hangzhou \
  --type ecs.e3.medium \
  --image aliyun_3_x64_20G_alibase_20240528.vhd \
  --sg sg-bp1234567890example \
  --vswitch vsw-bp1234567890example
```

Use Alibaba Cloud CLI when you need a raw operation that is not modeled as a
resource action, or use `ecctl call` when you want the same raw request through
the ecctl profile and output envelope:

Alibaba Cloud CLI:

```bash
aliyun ecs DescribeAvailableResource \
  --RegionId cn-hangzhou \
  --DestinationResource InstanceType \
  --IoOptimized optimized \
  --InstanceType ecs.e3.medium
```

ecctl:

```bash
ecctl call ecs DescribeAvailableResource \
  --region cn-hangzhou \
  --DestinationResource InstanceType \
  --IoOptimized optimized \
  --InstanceType ecs.e3.medium
```

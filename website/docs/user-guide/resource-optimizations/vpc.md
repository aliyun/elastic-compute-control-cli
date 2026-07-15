---
title: VPC Optimizations
description: Server-side DryRun, idempotency, and normalized list output for VPCs and vSwitches.
---

# VPC Optimizations

The resource IDs and response values below are illustrative. Replace them with
values from your account when running the commands.

## VPCs

### Server-side DryRun

OpenAPI reports a passed VPC deletion validation as the `DryRunOperation` error
sentinel, so Alibaba Cloud CLI exits with status 1. ecctl sends the same
service-side validation request, converts that sentinel to `dry_run: passed`,
and exits with status 0. Neither command deletes the VPC.

Alibaba Cloud CLI:

```bash
aliyun vpc DeleteVpc \
  --RegionId cn-hangzhou \
  --VpcId vpc-bp1234567890example \
  --DryRun true
```

ecctl:

```bash
ecctl vpc delete vpc-bp1234567890example \
  --region cn-hangzhou \
  --dry-run
```

The critical results show the different process contracts:

```json
// Alibaba Cloud CLI
ErrorCode: DryRunOperation
Message: Request validation has been passed with DryRun flag set.
...
Exit code: 1

// ecctl
{
  "actions": [{"action_name": "DeleteVpc", ...}],
  "dry_run": "passed",
  ...
}
Exit code: 0
```

### Idempotent create

OpenAPI and Alibaba Cloud CLI expose `ClientToken` directly. ecctl names the
same request identity `--idempotency-key` and can generate a token when the flag
is omitted. Use an explicit value when retries may run in another process.

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

Repeating either command with the same token reuses the same service-side
request identity.

### Normalized list output

`DescribeVpcs` returns VPCs under the `Vpcs.Vpc` wrapper with API-specific page
fields. `ecctl vpc list` maps the same data to a `vpcs` array and common page
metadata. The maximum page size still follows `DescribeVpcs`.

Alibaba Cloud CLI:

```bash
aliyun vpc DescribeVpcs \
  --RegionId cn-hangzhou \
  --PageNumber 1 \
  --PageSize 50
```

ecctl:

```bash
ecctl vpc list \
  --region cn-hangzhou \
  --filter status=Available \
  --page 1 \
  --limit 50
```

```json
// OpenAPI
{
  "Vpcs": {"Vpc": [{"VpcId": "vpc-bp1234567890example", "Status": "Available", ...}]},
  "PageNumber": 1,
  "PageSize": 50,
  "TotalCount": 1,
  ...
}

// ecctl
{
  "vpcs": [{"id": "vpc-bp1234567890example", "status": "Available", ...}],
  "pagination": {"page": 1, "limit": 50, "returned": 1, "has_more": false},
  ...
}
```

See the [VPC reference](../../reference/resources/vpc/vpc.md).

## vSwitches

### Server-side DryRun

OpenAPI uses the `DryRunOperation` error sentinel when a vSwitch deletion
validation passes. ecctl converts the same response to a successful dry-run
result and does not delete the vSwitch.

Alibaba Cloud CLI:

```bash
aliyun vpc DeleteVSwitch \
  --RegionId cn-hangzhou \
  --VSwitchId vsw-bp1234567890example \
  --DryRun true
```

ecctl:

```bash
ecctl vpc vswitch delete vsw-bp1234567890example \
  --region cn-hangzhou \
  --dry-run
```

```json
// Alibaba Cloud CLI
ErrorCode: DryRunOperation
...
Exit code: 1

// ecctl
{"actions": [{"action_name": "DeleteVSwitch", ...}], "dry_run": "passed", ...}
Exit code: 0
```

### Idempotent create

Alibaba Cloud CLI requires the raw `ClientToken`. ecctl exposes the same value
as `--idempotency-key` and can generate it when omitted. Reuse an explicit key
when a retry must keep its identity across process restarts.

Alibaba Cloud CLI:

```bash
aliyun vpc CreateVSwitch \
  --RegionId cn-hangzhou \
  --VpcId vpc-bp1234567890example \
  --ZoneId cn-hangzhou-h \
  --CidrBlock 10.0.1.0/24 \
  --ClientToken app-a-42
```

ecctl:

```bash
ecctl vpc vswitch create \
  --region cn-hangzhou \
  --vpc vpc-bp1234567890example \
  --zone cn-hangzhou-h \
  --cidr 10.0.1.0/24 \
  --idempotency-key app-a-42
```

### Normalized list output

`DescribeVSwitches` returns items under `VSwitches.VSwitch` and uses its own
page fields. ecctl returns matching vSwitches under `vswitches` with the common
pagination object. The service still controls the maximum page size.

Alibaba Cloud CLI:

```bash
aliyun vpc DescribeVSwitches \
  --RegionId cn-hangzhou \
  --VpcId vpc-bp1234567890example \
  --PageNumber 1 \
  --PageSize 50
```

ecctl:

```bash
ecctl vpc vswitch list \
  --region cn-hangzhou \
  --filter vpc=vpc-bp1234567890example \
  --page 1 \
  --limit 50
```

```json
// OpenAPI
{
  "VSwitches": {"VSwitch": [{"VSwitchId": "vsw-bp1234567890example", ...}]},
  "PageNumber": 1,
  "PageSize": 50,
  ...
}

// ecctl
{
  "vswitches": [{"id": "vsw-bp1234567890example", ...}],
  "pagination": {"page": 1, "limit": 50, "returned": 1, "has_more": false},
  ...
}
```

See the [vSwitch reference](../../reference/resources/vpc/vswitch.md).

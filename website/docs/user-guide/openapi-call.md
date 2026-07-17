---
title: OpenAPI Calls
description: Use ecctl call for operations that are not modeled as resources.
---

# OpenAPI Calls

Prefer resource commands when they exist. Use `ecctl call` for Alibaba Cloud
OpenAPI operations that are not modeled in the public resource surface.

`ecctl call` keeps raw OpenAPI operation names and request shapes. It does not
add resource-command behavior such as waiters, idempotency injection, or
response normalization.

## List Products

```bash
ecctl call --list --filter ecs --limit 3
```

Output:

```json
{
  "count": 1,
  "total": 1,
  "truncated": false,
  "filter": "ecs",
  "limit": 3,
  "products": [
    {
      "name": "ecs",
      "description": "Elastic Compute Service"
    }
  ]
}
```

## Inspect an Operation

```bash
ecctl call --schema ecs DescribeInstances --generate-request
```

Request template:

```json
{
  "RegionId": "<RegionId>"
}
```

## Call an Operation

Pass request parameters as JSON:

```bash
ecctl call ecs DescribeInstances --region cn-hangzhou --request '{"PageSize":10}'
```

Or pass OpenAPI parameters as flags:

```bash
ecctl call ecs DescribeInstances --region cn-hangzhou --PageSize 10
```

These command forms are accepted by `ecctl call`; executing them requires valid
Alibaba Cloud credentials and a region.

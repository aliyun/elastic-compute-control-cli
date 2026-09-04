---
title: Chainable RAM role ARN
description: Assume a RAM role using another named profile as the source credential.
---

# Chainable RAM role ARN

`ChainableRamRoleArn` assumes a role from a source *profile* rather than from
inline credentials. The source profile is resolved first, through any mode it
uses, and its credential becomes the source for `AssumeRole`.

This is how you build a chain: an [OAuth](./oauth.md) login assumes a role in a
shared account, which then assumes a role in a workload account. It also keeps
a single source credential in one place instead of duplicating an AccessKey
into every downstream profile.

## Configure with Alibaba Cloud CLI

```bash
aliyun configure --mode ChainableRamRoleArn --profile workload
```

`ecctl configure --mode ChainableRamRoleArn` is not supported. `--mode` accepts
`OAuth` only. Put this profile in the compatible `aliyun` configuration file.

## Profile fields

| Field | Required | Notes |
|---|---|---|
| `mode` | Yes in practice | `ChainableRamRoleArn`. Not inferred from other fields |
| `source_profile` | Yes | Name of another profile in the same compatible `aliyun` configuration file |
| `ram_role_arn` | Yes | Complete ARN: `acs:ram::<16-digit-account-id>:role/<role-name>` |
| `ram_session_name` | No | Falls back to `ALIBABA_CLOUD_ROLE_SESSION_NAME` |
| `expired_seconds` | No | Requested session duration in seconds |
| `policy` | No | Inline policy further narrowing the assumed session |
| `external_id` | No | Falls back to `ALIBABA_CLOUD_EXTERNAL_ID` |
| `sts_endpoint` | No | Custom STS endpoint. Must be HTTPS |
| `sts_region` | No | Regional STS endpoint selection |
| `enable_vpc` | No | Use the VPC STS endpoint |

There is no environment-variable form of this mode. `source_profile` names a
profile, which only exists in a configuration file.

A two-hop chain from a browser login to a workload account:

```json
{
  "current": "workload",
  "profiles": [
    {
      "name": "shared",
      "mode": "RamRoleArn",
      "access_key_id": "LTAI5t...",
      "access_key_secret": "...",
      "ram_role_arn": "acs:ram::1111111111111111:role/shared-admin",
      "region_id": "cn-hangzhou"
    },
    {
      "name": "workload",
      "mode": "ChainableRamRoleArn",
      "source_profile": "shared",
      "ram_role_arn": "acs:ram::2222222222222222:role/workload-deploy",
      "ram_session_name": "ecctl-session",
      "region_id": "cn-hangzhou"
    }
  ]
}
```

`source_profile` must name a profile in the same compatible `aliyun`
configuration file. A missing source fails with `source profile <name> not
found`, and an empty `source_profile` fails with `source_profile is required
for ChainableRamRoleArn`.

## Cycle detection

A chain that returns to a profile it has already visited is rejected locally:

```json
{
  "error": {
    "kind": "client",
    "code": "InvalidCredentials",
    "message": "credential profile chain contains a cycle at workload"
  }
}
```

Detection covers the whole resolution path, so a profile that is its own
indirect source fails immediately instead of recursing.

## ARN validation and identity verification

As with [RAM role ARN](./ram-role-arn.md), `ram_role_arn` must be a complete
`acs:ram::<16-digit-account-id>:role/<role-name>` ARN. `ecctl` derives the
expected account from it and verifies the initial credential through an
official STS `GetCallerIdentity` endpoint before the first business request. A
custom `sts_endpoint` may issue credentials but is never trusted to verify its
own result.

Every hop in the chain is renewable, and the first renewable credential pins
the canonical identity. A refresh that returns a different role is rejected
before it can sign a request.

## Verify

```bash
ecctl --profile workload configure get
ecctl --profile workload --region cn-hangzhou ecs region list
```

A failure at the source hop reports the source profile's own error. A failure
at the assume hop reports `refresh session token failed` with the STS response
body and a request ID.

## Related

- [RAM role ARN](./ram-role-arn.md) for assuming from inline credentials
- [Credentials overview](./index.md) for resolution order

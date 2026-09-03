---
title: RAM role ARN
description: Assume a RAM role from an AccessKey or STS source credential.
---

# RAM role ARN

`RamRoleArn` calls STS `AssumeRole` using a source credential and the resulting
temporary credentials for every signed request. Use it to cross an account
boundary, to narrow a broad source identity down to a specific role, or to
satisfy a policy that requires role assumption.

The result is renewable, so long operations keep working: `ecctl` refreshes
before a later signed request when the assumed credential is close to expiry.

## Configure with Alibaba Cloud CLI

```bash
aliyun configure --mode RamRoleArn --profile cross-account
```

`ecctl configure --mode RamRoleArn` is not supported. `--mode` accepts `OAuth`
only, and an ecctl-native profile resolves only OAuth or a static credential.
Put this profile in the compatible `aliyun` configuration file.

A trap worth naming: an ecctl-native profile that declares
`mode: RamRoleArn` but also carries `access_key_id` and `access_key_secret`
uses that access key pair directly and ignores `ram_role_arn`. No role is
assumed. Keep this profile in the compatible `aliyun` configuration file.

## Configure with environment variables

```bash
export ALIBABA_CLOUD_ACCESS_KEY_ID=<source-id>
export ALIBABA_CLOUD_ACCESS_KEY_SECRET=<source-secret>
export ALIBABA_CLOUD_ROLE_ARN=acs:ram::1234567890123456:role/demo
export ALIBABA_CLOUD_ROLE_SESSION_NAME=ecctl-session
```

In the environment chain, an access key pair plus `ALIBABA_CLOUD_ROLE_ARN`
becomes `RamRoleArn`. Without the role ARN the same pair stays
[`AK`](./ak.md). `ALIBABA_CLOUD_EXTERNAL_ID`,
`ALIBABA_CLOUD_STS_ENDPOINT`, and `ALIBABA_CLOUD_STS_REGION` are also read on
this path.

## Profile fields

| Field | Required | Notes |
|---|---|---|
| `mode` | No | `RamRoleArn`. Inferred when `ram_role_arn` is present alongside an access key pair |
| `access_key_id` | Yes | Source AccessKey ID. Falls back to the environment |
| `access_key_secret` | Yes | Source AccessKey secret. Falls back to the environment |
| `sts_token` | No | Present when the source is itself a temporary credential |
| `ram_role_arn` | Yes | Complete ARN: `acs:ram::<16-digit-account-id>:role/<role-name>` |
| `ram_session_name` | No | Role session name. Falls back to `ALIBABA_CLOUD_ROLE_SESSION_NAME` |
| `expired_seconds` | No | Requested session duration in seconds |
| `policy` | No | Inline policy further narrowing the assumed session |
| `external_id` | No | External ID required by the role trust policy |
| `sts_endpoint` | No | Custom STS endpoint. Must be HTTPS |
| `sts_region` | No | Regional STS endpoint selection |
| `enable_vpc` | No | Use the VPC STS endpoint |

```json
{
  "name": "cross-account",
  "mode": "RamRoleArn",
  "access_key_id": "LTAI5t...",
  "access_key_secret": "...",
  "ram_role_arn": "acs:ram::1234567890123456:role/demo",
  "ram_session_name": "ecctl-session",
  "expired_seconds": 3600,
  "region_id": "cn-hangzhou"
}
```

`ram_session_name`, `expired_seconds`, and `policy` are passed through as given.
When they are omitted, `ecctl` sends no value and the Alibaba Cloud credentials
SDK and STS apply their own defaults.

## ARN validation and identity verification

`ram_role_arn` must be a complete `acs:ram::<16-digit-account-id>:role/<role-name>`
ARN. `ecctl` derives the expected account from that ARN, then verifies the
initial credential through an official STS `GetCallerIdentity` endpoint before
the first business request. An ARN with a missing or malformed account segment
fails locally rather than producing a confusing service error.

An explicit custom `sts_endpoint` may issue credentials, but it is never
trusted to verify its own result. The independent identity check still goes to
an official endpoint. A custom endpoint must be an HTTPS host without user
information, path, query, or fragment:

```json
{
  "error": {
    "kind": "client",
    "code": "InvalidCredentials",
    "message": "STS endpoint must be an HTTPS host without user information, path, query, or fragment"
  }
}
```

Set `sts_region` and `enable_vpc` when the identity check must use a regional
or VPC STS endpoint.

## Verify

```bash
ecctl --profile cross-account configure get
ecctl --profile cross-account --region cn-hangzhou ecs region list
```

A source credential that STS rejects produces `refresh session token failed`
with the STS response body, which names `sts.aliyuncs.com` or the configured
regional endpoint and carries a request ID. That means resolution reached the
assume-role step.

## Related

- [Chainable RAM role ARN](./chainable-ram-role-arn.md) for assuming through a
  named source profile
- [OIDC](./oidc.md) for workload identity without a stored source key
- [Credentials overview](./index.md) for resolution order

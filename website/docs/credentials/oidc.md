---
title: OIDC
description: Exchange an OIDC token for renewable Alibaba Cloud credentials with ecctl.
---

# OIDC

`OIDC` exchanges an OIDC identity token for temporary Alibaba Cloud
credentials through STS `AssumeRoleWithOIDC`. No Alibaba Cloud secret is stored
anywhere: the OIDC token file is the only input, and it is typically issued and
rotated by the workload platform.

This is the right mode for Kubernetes pods using RRSA, GitHub Actions, GitLab
CI, or any other system that mints OIDC tokens for its jobs. The credential is
renewable, so long operations refresh themselves.

## Configure with Alibaba Cloud CLI

```bash
aliyun configure --mode OIDC --profile rrsa
```

`ecctl configure --mode OIDC` is not supported. `--mode` accepts `OAuth` only,
and an ecctl-native profile resolves only OAuth or a static credential. Put
this profile in the Aliyun-compatible configuration file.

## Configure with environment variables

```bash
export ALIBABA_CLOUD_OIDC_PROVIDER_ARN=acs:ram::1234567890123456:oidc-provider/ack-rrsa-c1234
export ALIBABA_CLOUD_OIDC_TOKEN_FILE=/var/run/secrets/tokens/oidc-token
export ALIBABA_CLOUD_ROLE_ARN=acs:ram::1234567890123456:role/pod-role
export ALIBABA_CLOUD_ROLE_SESSION_NAME=ecctl-session
```

All three of the provider ARN, token file, and role ARN are required together.
In the environment chain this set is tested after an access key pair and before
`ALIBABA_CLOUD_ECS_METADATA`.

A partially populated set is a hard error, not a silent skip:

```json
{
  "error": {
    "kind": "client",
    "code": "InvalidCredentials",
    "message": "ALIBABA_CLOUD_OIDC_PROVIDER_ARN, ALIBABA_CLOUD_OIDC_TOKEN_FILE, and ALIBABA_CLOUD_ROLE_ARN are all required for OIDC credentials"
  }
}
```

That ordering matters in practice. A pod that sets the provider ARN and role
ARN but has a missing or unmounted token file fails with this message instead
of quietly falling through to instance metadata.

`ALIBABA_CLOUD_STS_ENDPOINT`, `ALIBABA_CLOUD_STS_REGION`, and
`ALIBABA_CLOUD_VPC_ENDPOINT_ENABLED` are also read on this path.

## Profile fields

| Field | Required | Notes |
|---|---|---|
| `mode` | No | `OIDC`. Inferred when `oidc_provider_arn` is present |
| `oidc_provider_arn` | Yes | `acs:ram::<16-digit-account-id>:oidc-provider/<name>` |
| `oidc_token_file` | Yes | Path to the projected token |
| `ram_role_arn` | Yes | `acs:ram::<16-digit-account-id>:role/<role-name>` |
| `ram_session_name` | No | Falls back to `ALIBABA_CLOUD_ROLE_SESSION_NAME` |
| `expired_seconds` | No | Requested session duration in seconds |
| `policy` | No | Inline policy further narrowing the assumed session |
| `sts_endpoint` | No | Custom STS endpoint. Must be HTTPS |
| `sts_region` | No | Regional STS endpoint selection |
| `enable_vpc` | No | Use the VPC STS endpoint |

```json
{
  "name": "rrsa",
  "mode": "OIDC",
  "oidc_provider_arn": "acs:ram::1234567890123456:oidc-provider/ack-rrsa-c1234",
  "oidc_token_file": "/var/run/secrets/tokens/oidc-token",
  "ram_role_arn": "acs:ram::1234567890123456:role/pod-role",
  "ram_session_name": "ecctl-session",
  "region_id": "cn-hangzhou"
}
```

## Validation

All three fields are required together:

```json
{
  "error": {
    "kind": "client",
    "code": "InvalidCredentials",
    "message": "oidc_provider_arn, oidc_token_file, and ram_role_arn are required for OIDC credentials"
  }
}
```

Both ARNs must be complete and well formed, and they must belong to the same
account:

```json
{
  "error": {
    "kind": "client",
    "code": "InvalidCredentials",
    "message": "OIDC provider ARN and RAM role ARN must belong to the same account"
  }
}
```

These checks run locally, before the token file is read. That is the point of
the same-account rule: `ecctl` validates derived STS endpoints before reading
or transmitting an OIDC token, so a mismatched configuration cannot cause the
token to be presented to the wrong account's endpoint.

As with [RAM role ARN](./ram-role-arn.md), `ecctl` derives the expected account
from the role ARN and verifies the initial credential through an official STS
`GetCallerIdentity` endpoint before the first business request. A custom
`sts_endpoint` may issue credentials but is never trusted to verify its own
result.

## Verify

```bash
ecctl --profile rrsa configure get
ecctl --profile rrsa --region cn-hangzhou ecs region list
```

Run the check inside the workload. From outside it, the projected token file
usually does not exist, and the failure says nothing about the configuration.

## Renewal and token rotation

The assumed credential is renewable, and `ecctl` refreshes before a later
signed request when it is close to expiry. Each refresh re-reads the token
file, so a platform that rotates the projected token keeps working through a
long operation without intervention.

The first credential pins the canonical account and role. A refreshed
credential for a different identity is rejected before it can sign a request.

## Related

- [ECS RAM role](./ecs-ram-role.md) for workloads on ECS without OIDC
- [RAM role ARN](./ram-role-arn.md) for assuming from a stored source credential
- [Credentials overview](./index.md)

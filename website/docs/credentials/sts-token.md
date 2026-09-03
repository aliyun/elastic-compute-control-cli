---
title: STS token
description: Use an existing temporary AccessKey, secret, and security token with ecctl.
---

# STS token

`StsToken` carries temporary credentials issued elsewhere: an AccessKey ID, an
AccessKey secret, and a security token. `ecctl` uses them exactly as given and
cannot refresh them. When they expire, the command fails and something outside
`ecctl` must issue a new set.

Use this mode when another system already vends short-lived credentials, such
as a CI job that calls STS `AssumeRole` itself. When `ecctl` should do the
assuming, use [RAM role ARN](./ram-role-arn.md) instead. When the credentials
should be fetched and renewed from an endpoint, use
[Credentials URI](./credentials-uri.md).

## Configure with ecctl

```bash
ecctl configure set access-key-id <temporary-id>
ecctl configure set access-key-secret <temporary-secret>
ecctl configure set security-token <token>
```

`security-token` is stored as `sts_token`. Setting it on a profile that already
holds an access key pair switches that profile from `AK` to `StsToken`.

`ecctl configure --mode StsToken` is not supported. `--mode` accepts `OAuth`
only.

## Configure with Alibaba Cloud CLI

```bash
aliyun configure --mode StsToken --profile ci
```

## Configure with environment variables

```bash
export ALIBABA_CLOUD_ACCESS_KEY_ID=<temporary-id>
export ALIBABA_CLOUD_ACCESS_KEY_SECRET=<temporary-secret>
export ALIBABA_CLOUD_SECURITY_TOKEN=<token>
```

All three are required. An access key pair without a security token resolves as
[`AK`](./ak.md), not as a partial `StsToken`. Each variable also accepts an
`ALIBABACLOUD_`, `ALICLOUD_`, or bare spelling.

## Profile fields

| Field | Required | Notes |
|---|---|---|
| `mode` | No | `StsToken`. Inferred when `sts_token` is present alongside an access key pair |
| `access_key_id` | Yes | Temporary AccessKey ID, usually prefixed `STS.` |
| `access_key_secret` | Yes | Temporary AccessKey secret |
| `sts_token` | Yes | The security token |
| `sts_expiration` | No | Unix timestamp in seconds. Enables expiry checking |

```json
{
  "name": "ci",
  "mode": "StsToken",
  "access_key_id": "STS.NUgYrLnoC...",
  "access_key_secret": "...",
  "sts_token": "...",
  "sts_expiration": 1782460800,
  "region_id": "cn-hangzhou"
}
```

`sts_expiration` is a Unix timestamp in seconds, not an RFC 3339 string.

## Expiration behavior

Without `sts_expiration`, `ecctl` sends the credential and lets the service
decide. An expired token then fails at the API with a service-side error.

With `sts_expiration`, `ecctl` rejects an expired token before signing, and
rejects a token that cannot cover a known command deadline. This turns a
mid-transfer failure into an immediate local error, which is much easier to
diagnose during a long operation.

Because `StsToken` cannot refresh itself, a long OSS transfer needs a
`sts_expiration` comfortably beyond the transfer window. When you cannot
guarantee that, switch to a renewable mode: [OAuth](./oauth.md),
[OIDC](./oidc.md), [RAM role ARN](./ram-role-arn.md),
[ECS RAM role](./ecs-ram-role.md), [External](./external.md), or
[Credentials URI](./credentials-uri.md).

## Verify

```bash
ecctl --profile ci configure get
ecctl --profile ci --region cn-hangzhou ecs region list
```

A malformed security token is rejected by the service, so it surfaces as a
service error with code `CloudAPIError` naming the response, for example
`Specified SecurityToken is malformed`. That response carries a request ID,
which means resolution and signing succeeded locally and the service rejected
the token itself. A client-side `InvalidCredentials` or `MissingCredentials`
error, with no request ID, means the credential never resolved.

## Related

- [AccessKey](./ak.md) for long-lived keys
- [Credentials URI](./credentials-uri.md) for renewable STS over HTTP
- [Credentials overview](./index.md) for resolution order

---
title: Credentials URI
description: Fetch renewable STS credentials from an HTTPS or loopback HTTP endpoint.
---

# Credentials URI

`CredentialsURI` fetches temporary credentials from an HTTP endpoint instead of
running a local program. Use it when a credential broker is reachable over the
network, when a sidecar vends credentials on localhost, or when a container
platform injects a credential URL into the environment.

The credential is renewable, so `ecctl` re-fetches before a later signed
request as it approaches expiry.

## Configure with Alibaba Cloud CLI

```bash
aliyun configure --mode CredentialsURI --profile broker
```

`ecctl configure --mode CredentialsURI` is not supported. `--mode` accepts
`OAuth` only, and an ecctl-native profile resolves only OAuth or a static
credential. A profile in the ecctl configuration file that declares
`CredentialsURI` without static credentials fails with `MissingCredentials`.
Put this profile in the Aliyun-compatible configuration file.

## Configure with environment variables

```bash
export ALIBABA_CLOUD_CREDENTIALS_URI=https://broker.internal/credentials
```

The environment path is consulted only when no stored profile is selected, or
when `ALIBABA_CLOUD_IGNORE_PROFILE=TRUE` forces the environment-only path. A
matched Aliyun-compatible profile never falls back to environment credentials,
even when the profile itself carries none. In the environment chain this source
is tested after an access key pair, a complete OIDC set, and
`ALIBABA_CLOUD_ECS_METADATA`, and before `ALIBABA_CLOUD_BEARER_TOKEN`.

## Profile fields

| Field | Required | Notes |
|---|---|---|
| `mode` | No | `CredentialsURI`. Inferred when `credentials_uri` is present |
| `credentials_uri` | Yes | Falls back to `ALIBABA_CLOUD_CREDENTIALS_URI` when empty |

```json
{
  "name": "broker",
  "mode": "CredentialsURI",
  "credentials_uri": "https://broker.internal/credentials?role=ecctl",
  "region_id": "cn-hangzhou"
}
```

## Transport requirements

HTTPS is required, with one exception: a URL whose host is a literal loopback
IP address may use HTTP.

```json
{
  "error": {
    "kind": "client",
    "code": "InvalidCredentials",
    "message": "credentials URI requires HTTPS unless it uses a literal loopback address"
  }
}
```

`http://127.0.0.1:8080/credentials` and `http://[::1]:8080/credentials` are
accepted. `http://localhost:8080/credentials` is not, because `localhost` is a
hostname that DNS could point anywhere. This makes a localhost sidecar
workable while keeping credentials off plaintext transport for anything that
leaves the machine.

## Response contract

The endpoint must return HTTP 200 with a JSON body:

| Field | Required | Notes |
|---|---|---|
| `Code` | Yes | Must be exactly `Success` |
| `AccessKeyId` | Yes | |
| `AccessKeySecret` | Yes | |
| `SecurityToken` | Yes | Always required, unlike the External helper contract |
| `Expiration` | Yes | RFC 3339 UTC, must be in the future |

```json
{
  "Code": "Success",
  "AccessKeyId": "STS.NUgYrLnoC...",
  "AccessKeySecret": "...",
  "SecurityToken": "...",
  "Expiration": "2026-09-03T12:00:00Z"
}
```

Note the field naming: this contract uses PascalCase, unlike the
[External](./external.md) helper contract, which uses snake_case.

Failures and their messages, where `<source>` is the URI with its path
stripped:

| Condition | Message |
|---|---|
| Non-200 status | `credential source <source> returned HTTP <code>` |
| `Code` is not `Success` | `credential source <source> returned incomplete credentials` |
| Any of `AccessKeyId`, `AccessKeySecret`, `SecurityToken` empty | `credential source <source> returned incomplete credentials` |
| `Expiration` missing or unparseable | `credential source <source> returned an invalid expiration` |
| `Expiration` not in the future | `credential source <source> returned expired credentials` |

A non-`Success` `Code` and a missing field produce the same message, so an
endpoint that returns a structured failure body looks like an incomplete
response. Check the endpoint's own logs when you see it.

`Expiration` is mandatory here. A CredentialsURI response without a valid
future expiration is always rejected, which is what allows `ecctl` to treat the
credential as renewable and schedule its own refresh.

## Disabling this source

```bash
export ALIBABA_CLOUD_DISABLE_EXTERNAL_PROCESS=true
```

```json
{
  "error": {
    "kind": "client",
    "code": "CredentialSourceDisabled",
    "message": "ALIBABA_CLOUD_DISABLE_EXTERNAL_PROCESS disables CredentialsURI credentials"
  }
}
```

The variable accepts `1` or `true`, case-insensitively, and disables
[External](./external.md) as well. Set it where a configuration file or the
environment might be influenced by someone else, so an injected URL cannot be
contacted.

## Verify

```bash
ecctl --profile broker configure get
ecctl --profile broker --region cn-hangzhou ecs region list
```

Check the endpoint independently first:

```bash
curl -s https://broker.internal/credentials?role=ecctl
```

Confirm the body carries `Code: "Success"`, all four credential fields, and an
`Expiration` far enough in the future to cover the operation you are about to
run.

## Renewal and identity pinning

`ecctl` re-fetches before a later signed request as the credential approaches
expiry, so long operations keep working. The first credential pins the
canonical identity; a later fetch that returns a different identity is rejected
before it can sign a request.

## Related

- [External process](./external.md) for the local-program equivalent
- [STS token](./sts-token.md) for a fixed temporary credential
- [Credentials overview](./index.md)

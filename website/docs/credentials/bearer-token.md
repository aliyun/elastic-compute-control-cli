---
title: Bearer token
description: Authenticate product APIs that accept bearer tokens with ecctl.
---

# Bearer token

`BearerToken` sends a token in a request header instead of signing the request
with an access key. It applies only to the product APIs that accept bearer
authentication. It is not a general-purpose credential and is not a way to work
around a missing access key.

The token is static. `ecctl` cannot renew it, and there is no expiration field
to check.

## Configure with Alibaba Cloud CLI

```bash
aliyun configure --mode BearerToken --profile bearer
```

`ecctl configure --mode BearerToken` is not supported. `--mode` accepts `OAuth`
only, and an ecctl-native profile resolves only OAuth or a static credential.
Put this profile in the compatible `aliyun` configuration file.

## Configure with environment variables

```bash
export ALIBABA_CLOUD_BEARER_TOKEN=<token>
export ALIBABA_CLOUD_BEARER_TOKEN_HEADER_KEY=<header-name>
```

The header variable is optional. In the environment chain this is the last
source consulted, after an access key pair, a complete OIDC set,
`ALIBABA_CLOUD_ECS_METADATA`, and `ALIBABA_CLOUD_CREDENTIALS_URI`. Anything
that resolves earlier wins.

## Profile fields

| Field | Required | Notes |
|---|---|---|
| `mode` | No | `BearerToken`. Inferred when `bearer_token` is present |
| `bearer_token` | Yes | Falls back to `ALIBABA_CLOUD_BEARER_TOKEN` when empty |
| `bearer_token_header_key` | No | Defaults to `x-acs-bearer-token` |

```json
{
  "name": "bearer",
  "mode": "BearerToken",
  "bearer_token": "...",
  "bearer_token_header_key": "x-acs-bearer-token",
  "region_id": "cn-hangzhou"
}
```

Set `bearer_token_header_key` only when the target API documents a different
header name. The default is what most bearer-authenticated product APIs expect.

## Scope limitation

A bearer token is accepted only by APIs that support bearer authentication. An
API that does not support it rejects the request at the service, in wording
that comes from the service rather than from `ecctl`:

```json
{
  "error": {
    "kind": "service",
    "code": "CloudAPIError",
    "message": "API call failed; see actions for details"
  },
  "actions": [
    {
      "action_name": "DescribeRegions",
      "code": "403, This signature type is not supported.",
      "message": "code: 403, This signature type is not supported."
    }
  ]
}
```

This response is the clearest signal that the mode is functioning as designed.
The token was resolved, attached to the request, and delivered; the target API
simply does not accept bearer authentication. Note the `kind` is `service`, not
`client`: `ecctl` did not fail to produce a credential.

`This signature type is not supported` is not a configuration problem to fix in
the profile. It means the operation you called is not a bearer-authenticated
operation. Use a signing mode such as [OAuth](./oauth.md),
[AK](./ak.md), or [RAM role ARN](./ram-role-arn.md) for general resource
commands, and reserve `BearerToken` for the API that asked for it.

## Verify

```bash
ecctl --profile bearer configure get
```

`configure get` has no key for the bearer token. The keys it accepts are
`region`, `access-key-id`, `access-key-secret`, `security-token`, `lang`,
`output`, and `telemetry.enabled`, so asking for `bearer_token` returns a client
error with code `UnknownConfigKey`. Read the value back from the configuration
file instead, and only when you deliberately need it:

```bash
grep bearer_token ~/.aliyun/config.json
```

Because there is no local validity check, the only real verification is a
request to an API that accepts bearer authentication. A 403 from an unrelated
API tells you nothing about whether the token is valid.

## Related

- [STS token](./sts-token.md) for temporary signed credentials
- [Credentials overview](./index.md) for mode selection

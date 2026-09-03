---
title: AccessKey
description: Configure a long-lived Alibaba Cloud access key pair for ecctl.
---

# AccessKey

`AK` uses a long-lived AccessKey ID and AccessKey secret. It is the simplest
mode and the easiest to misuse: the secret does not expire, grants everything
its RAM policy allows, and sits on disk wherever you store it.

Prefer [OAuth](./oauth.md) for interactive work and a renewable mode such as
[OIDC](./oidc.md), [ECS RAM role](./ecs-ram-role.md), or
[RAM role ARN](./ram-role-arn.md) for automation. Use `AK` when a renewable
mode is genuinely unavailable.

`AK` is not renewable. For long OSS transfers or long scripts, a static key is
fine because it never expires mid-operation, but it is also never rotated
automatically.

## Configure with ecctl

```bash
ecctl configure set access-key-id <id>
ecctl configure set access-key-secret <secret>
```

Each command writes the selected profile in `~/.ecctl/config.json` and echoes
the stored key:

```json
{
  "key": "access-key-secret",
  "profile": "default",
  "sensitive": true,
  "value": "********"
}
```

Setting only these two fields produces an `AK` profile. Adding
`security-token` later flips the same profile to
[`StsToken`](./sts-token.md).

Write to a named profile with `--profile`:

```bash
ecctl --profile production configure set access-key-id <id>
ecctl --profile production configure set access-key-secret <secret>
```

`ecctl configure --mode AK` is not supported. `--mode` accepts `OAuth` only;
the `AK` mode is derived from the presence of an access key pair.

## Configure with Alibaba Cloud CLI

`ecctl` reads a compatible `aliyun` configuration as a read-only fallback, so
an existing profile keeps working:

```bash
aliyun configure --mode AK --profile production
```

## Configure with environment variables

```bash
export ALIBABA_CLOUD_ACCESS_KEY_ID=<id>
export ALIBABA_CLOUD_ACCESS_KEY_SECRET=<secret>
```

Environment credentials are consulted when no stored profile is selected, when
the selected ecctl profile carries no credentials, or when
`ALIBABA_CLOUD_IGNORE_PROFILE=TRUE` forces the environment-only path. Setting
only one of the two variables is a hard error rather than a partial credential:

```json
{
  "error": {
    "kind": "client",
    "code": "InvalidCredentials",
    "message": "both ALIBABA_CLOUD_ACCESS_KEY_ID and ALIBABA_CLOUD_ACCESS_KEY_SECRET are required"
  }
}
```

Each variable also accepts an `ALIBABACLOUD_`, `ALICLOUD_`, or bare
`ACCESS_KEY_ID` / `ACCESS_KEY_SECRET` spelling.

## Profile fields

| Field | Required | Notes |
|---|---|---|
| `mode` | No | `AK`. Inferred from `access_key_id` and `access_key_secret` when absent |
| `access_key_id` | Yes | Falls back to the environment when empty |
| `access_key_secret` | Yes | Sensitive. Falls back to the environment when empty |

A profile in the Aliyun-compatible file that omits `mode` is classified by its
fields. A profile carrying an access key pair and no `sts_token` or
`ram_role_arn` resolves as `AK`.

```json
{
  "name": "production",
  "mode": "AK",
  "access_key_id": "LTAI5t...",
  "access_key_secret": "...",
  "region_id": "cn-hangzhou"
}
```

## Verify

```bash
ecctl --profile production configure get
ecctl --profile production --region cn-hangzhou ecs region list
```

`configure get` reports the declared mode without a cloud call. The second
command proves the key signs a real request. An unknown or disabled key returns
`404, Specified access key is not found`; that response carries a request ID,
which means resolution and signing succeeded and the service rejected the key
itself.

## Storage and permissions

A newly created ecctl configuration file gets `0600`. A write to a file that
already exists preserves the permissions that file already had, so an
`0644` config stays `0644` across every later `configure set`. Check it and
tighten it yourself if it is too open:

```bash
ls -l ~/.ecctl/config.json
chmod 600 ~/.ecctl/config.json
```

The private credential store under `~/.ecctl/credentials-v2/`, which holds
rotated OAuth and CloudSSO tokens and cached STS credentials, is always written
with canonical current-user-only permissions.

Read the stored secret back only when you deliberately need it:

```bash
ecctl configure get access-key-secret --show-secret
```

Secrets are masked as `********` by default in `configure get`, `configure
list`, and every other output path.

## Related

- [STS token](./sts-token.md) for temporary access key credentials
- [Credentials overview](./index.md) for resolution order
- [Configuration](../getting-started/configuration.md#supported-keys) for the
  settable key list

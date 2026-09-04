---
title: Credentials
description: Choose, configure, and verify the Alibaba Cloud credential modes that ecctl accepts.
---

# Credentials

`ecctl` accepts the same eleven credential modes as Alibaba Cloud CLI, so an
existing `aliyun` profile keeps working without changes. This section explains
where each mode is configured, how `ecctl` picks one when several are present,
and how to confirm the selection before a command signs a request.

Read this page first, then open the page for the mode you intend to use.

## Choose a mode

| Mode | Credential lifetime | Typical use | Page |
|---|---|---|---|
| `OAuth` | Renewable | Browser login for a local human user | [OAuth](./oauth.md) |
| `AK` | Static | Long-lived AccessKey pair | [AccessKey](./ak.md) |
| `StsToken` | Fixed expiry | A temporary token issued elsewhere | [STS token](./sts-token.md) |
| `EcsRamRole` | Renewable | Workload on an ECS instance with an attached RAM role | [ECS RAM role](./ecs-ram-role.md) |
| `RamRoleArn` | Renewable | Assume a role from an AK or STS source credential | [RAM role ARN](./ram-role-arn.md) |
| `ChainableRamRoleArn` | Renewable | Assume a role through a named source profile | [Chainable RAM role ARN](./chainable-ram-role-arn.md) |
| `OIDC` | Renewable | Kubernetes RRSA or another OIDC workload identity | [OIDC](./oidc.md) |
| `CloudSSO` | Renewable | Enterprise CloudSSO access configuration | [CloudSSO](./cloudsso.md) |
| `External` | Depends on helper | A local program that emits credentials | [External process](./external.md) |
| `CredentialsURI` | Renewable | An HTTPS or loopback HTTP credential endpoint | [Credentials URI](./credentials-uri.md) |
| `BearerToken` | Static | Product APIs that accept bearer authentication | [Bearer token](./bearer-token.md) |

Prefer a renewable mode for anything long-running. A renewable credential is
refreshed before a later signed request when it is close to expiry, so a
multi-hour OSS transfer or a long `ecctl` script does not fail partway through.
`AK`, `StsToken`, and `BearerToken` cannot refresh themselves.

## Where ecctl provisions each mode

`ecctl` resolves all eleven modes, but it does not offer an interactive setup
flow for all of them. `ecctl configure --mode` accepts `OAuth` only; any other
value is rejected:

```bash
ecctl configure --mode AK --profile production
```

```json
{
  "error": {
    "kind": "client",
    "code": "UnsupportedCredentialMode",
    "message": "credential mode AK is not supported by ecctl configure",
    "retryable": false,
    "accepted_values": ["OAuth"]
  }
}
```

The provisioning path therefore depends on the mode:

| Mode | How to provision it |
|---|---|
| `OAuth` | `ecctl configure --mode OAuth` |
| `AK`, `StsToken` | `ecctl configure set access-key-id`, `access-key-secret`, `security-token`, or `aliyun configure --mode AK` |
| `CloudSSO` | `aliyun configure --mode CloudSSO` |
| All others | `aliyun configure --mode <Mode>`, environment variables, or a profile written to a compatible `aliyun` configuration file |

## Configuration files

`ecctl` reads two configuration files and writes renewable state to a third
location.

| Path | Environment override | Role |
|---|---|---|
| `~/.ecctl/config.json` | `ECCTL_CONFIG_PATH` | ecctl metadata: region, language, output, active profile, native OAuth login state, and local AK or STS credentials |
| `~/.aliyun/config.json` | `ECCTL_ALIYUN_CONFIG_PATH`, then `ALIBABA_CLOUD_CONFIG_PATH`, `ALIBABACLOUD_CONFIG_PATH`, `ALICLOUD_CONFIG_PATH` | Compatible Alibaba Cloud CLI profiles, read-only during cloud commands |
| `~/.ecctl/credentials-v2/` | none | Private store for rotated OAuth and CloudSSO tokens and cached STS credentials |

Both configuration files use the same JSON shape:

```json
{
  "current": "default",
  "profiles": [
    {
      "name": "default",
      "mode": "OAuth",
      "region_id": "cn-hangzhou"
    }
  ]
}
```

The private store uses the home directory resolved for the current process and
does not move with `ECCTL_CONFIG_PATH`. Its entries are written with
current-user-only permissions. Renewable OAuth and CloudSSO state is never
written into the compatible `aliyun` configuration file.

Two capabilities differ between the files, and the difference matters:

- A profile in the **compatible `aliyun`** configuration file can use any of
  the eleven modes.
- A profile in the **ecctl** configuration file resolves only native `OAuth` (a
  profile with `mode: OAuth` and a non-empty `oauth_generation`) or a static
  credential (a profile carrying `access_key_id`, `access_key_secret`, or
  `sts_token`).

An ecctl profile that declares another mode is not resolved through that mode.
A profile that also carries static credentials uses those credentials directly
and ignores the rest, so `ram_role_arn` on an ecctl profile is silently
ineffective. A profile that declares `External` or `CredentialsURI` without
static credentials is routed to the environment credential chain: it resolves
from environment credentials when those are present, and reports
`MissingCredentials` only when the environment carries none either. Put any
non-static, non-OAuth profile in the compatible `aliyun` configuration file.

## Resolution order

Profile selection uses this order:

1. `--profile`
2. `ECCTL_PROFILE`, then `ALIBABACLOUD_PROFILE`, `ALIBABA_CLOUD_PROFILE`,
   `ALICLOUD_PROFILE`
3. the `current` value in local configuration

Once a profile name is chosen, credential resolution follows this order:

1. `ALIBABA_CLOUD_IGNORE_PROFILE=TRUE` short-circuits everything and uses only
   environment credentials.
2. An ecctl profile that is a native OAuth login.
3. An ecctl profile that carries a static credential override.
4. An compatible `aliyun` profile of the same name, resolved through the full
   eleven-mode chain.
5. An ecctl profile with no credentials falls back to environment credentials.
6. An explicitly requested profile, or a stored `current` profile, that matches
   neither file fails with `ProfileNotFound`.
7. With no profile in play, environment credentials are used.

Two consequences are worth internalizing:

- A matched profile never switches to a *different* credential source. A profile
  that declares `CredentialsURI` but carries no `credentials_uri`, with no
  `ALIBABA_CLOUD_CREDENTIALS_URI` in the environment either, fails with
  `InvalidCredentials`. It does not quietly degrade to an AccessKey pair that
  happens to be exported.
- Within its own source, a matched profile does fill gaps from the environment.
  The `AK`, `StsToken`, and `RamRoleArn` branches take each field from the
  profile first and from the matching environment variable second, and mode
  inference does the same when a profile declares no mode at all. A matched
  profile that declares `AK` and leaves `access_key_id` and `access_key_secret`
  empty therefore resolves to the environment AccessKey pair. `credentials_uri`
  and `bearer_token` fall back to their own environment variables the same way.

Selecting a profile does not switch off environment credentials. If you need a
command to use only what the profile itself carries, unset the credential
variables for that invocation.

### Environment credential chain

When `ecctl` uses environment credentials, it tests the sources in this order:

1. `ALIBABA_CLOUD_ACCESS_KEY_ID` with `ALIBABA_CLOUD_ACCESS_KEY_SECRET`,
   becoming `StsToken` when `ALIBABA_CLOUD_SECURITY_TOKEN` is also present and
   `RamRoleArn` when `ALIBABA_CLOUD_ROLE_ARN` is also present. Setting only one
   of the AccessKey pair is a hard error.
2. `OIDC`, requiring all three of `ALIBABA_CLOUD_OIDC_PROVIDER_ARN`,
   `ALIBABA_CLOUD_OIDC_TOKEN_FILE`, and `ALIBABA_CLOUD_ROLE_ARN`.
3. `EcsRamRole` from `ALIBABA_CLOUD_ECS_METADATA`.
4. A partially populated OIDC set is a hard error rather than a silent skip.
5. `CredentialsURI` from `ALIBABA_CLOUD_CREDENTIALS_URI`.
6. `BearerToken` from `ALIBABA_CLOUD_BEARER_TOKEN`.
7. Otherwise `MissingCredentials`.

The prefix aliases are not uniform across this chain:

| Variables | Accepted prefixes |
|---|---|
| `ACCESS_KEY_ID`, `ACCESS_KEY_SECRET`, `SECURITY_TOKEN` | `ALIBABA_CLOUD_`, `ALIBABACLOUD_`, `ALICLOUD_`, and bare |
| `ROLE_ARN`, `OIDC_PROVIDER_ARN`, `OIDC_TOKEN_FILE`, `EXTERNAL_ID` | `ALIBABA_CLOUD_` and `ALIBABACLOUD_` |
| `ROLE_SESSION_NAME`, `STS_ENDPOINT`, `STS_REGION`, `VPC_ENDPOINT_ENABLED`, `ECS_METADATA`, `IMDSV1_DISABLED`, `CREDENTIALS_URI`, `BEARER_TOKEN`, `BEARER_TOKEN_HEADER_KEY` | `ALIBABA_CLOUD_` only |

The gap is easy to trip over. `ALICLOUD_ROLE_ARN` is never read, so exporting it
alongside an AccessKey pair does not select `RamRoleArn`. The pair resolves as
plain `AK` and the command runs as the RAM user itself rather than the assumed
role, with no warning.

## Verify a selection

`ecctl configure get` reports the effective profile without making a cloud
call:

```bash
ecctl --profile production configure get
```

```json
{
  "lang": "",
  "mode": "OAuth",
  "output": "json",
  "profile": "production",
  "region": "cn-hangzhou"
}
```

This reports what the profile declares. It does not prove that the credential
resolves. For a mode that reaches the network, confirm with a cheap read
operation:

```bash
ecctl --profile production --region cn-hangzhou ecs region list
```

A credential problem surfaces as an `InvalidCredentials`, `MissingCredentials`,
`ProfileNotFound`, or `CredentialSourceDisabled` client error before any
resource action runs. An error that names an Alibaba Cloud API and carries a
request ID means the credential resolved, was signed, and reached the service.

Sensitive values are masked by default. Read one deliberately with
`--show-secret`:

```bash
ecctl configure get access-key-secret --show-secret
```

`configure set` takes the value as a positional argument and has no interactive
prompt. A secret typed on the command line is written to your shell history and
is visible to other local processes through `ps` while the command runs. On a
shared machine, prefer `OAuth`, or put the value in the configuration file
directly.

## Identity pinning

`ecctl` keeps the selected credential provider for the complete command and
refreshes temporary credentials before later signed requests when needed. The
first renewable credential pins the canonical account, user, or role. A later
credential for a different identity is rejected before it can sign a request,
so a rotated token cannot switch accounts mid-command.

`RamRoleArn`, `ChainableRamRoleArn`, and `OIDC` profiles must use a complete
`acs:ram::<16-digit-account-id>:role/<role-name>` ARN. `ecctl` derives the
expected account from that ARN and verifies the initial credential through an
official STS `GetCallerIdentity` endpoint before the first business request. A
custom `sts_endpoint` may issue credentials, but it is never trusted to verify
its own result. Set `sts_region` and `enable_vpc` when that independent
identity check must use a regional or VPC STS endpoint.

## Debug logging

The upstream Dara request logger prints signed URLs and headers before the
final `ecctl` HTTP client can redact them. Credential-bearing commands
therefore fail closed when the comma-separated `DEBUG` environment variable
contains the exact token `dara`. Remove that token before retrying.

## Related

- [Configuration](../getting-started/configuration.md) for region, language,
  output, and profile management
- [Environment variables](../getting-started/configuration.md#environment-variables)
  for the full override list
- [Error model](../reference/errors.md) for how credential failures are reported

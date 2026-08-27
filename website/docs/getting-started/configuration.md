---
title: Configuration
description: Configure profiles, credentials, region, language, and output.
---

# Configuration

`ecctl configure` writes local configuration for resource commands. It can also
read a compatible local `aliyun` CLI configuration, overlaying `ecctl`
configuration on top when both exist.

## Configure a Region

```bash
ecctl configure set region cn-hangzhou
```

Expected shape:

```json
{
  "key": "region",
  "profile": "default",
  "sensitive": false,
  "value": "cn-hangzhou"
}
```

Set the default output mode:

```bash
ecctl configure set output json
```

Read the effective profile:

```bash
ecctl configure get
```

Expected shape:

```json
{
  "lang": "",
  "mode": "",
  "output": "json",
  "profile": "default",
  "region": "cn-hangzhou"
}
```

## Credentials

`ecctl` accepts the same current credential modes as Alibaba Cloud CLI when it
reads `~/.aliyun/config.json`:

| Mode | Typical use |
|---|---|
| `OAuth` | Browser-authenticated local users; cached tokens refresh automatically |
| `EcsRamRole` | ECS instance RAM roles through IMDS |
| `RamRoleArn` | Assume a RAM role from AK or STS source credentials |
| `ChainableRamRoleArn` | Assume roles through a named source profile chain |
| `OIDC` | OIDC/RRSA workload identity |
| `CloudSSO` | CloudSSO access configurations |
| `External` | Run an argv-based credential helper without a shell |
| `CredentialsURI` | Retrieve renewable STS credentials from HTTPS or a loopback HTTP endpoint |
| `StsToken` | Existing temporary AK, secret, and security token |
| `BearerToken` | Product APIs that accept bearer authentication |
| `AK` | Long-lived access key credentials |

Configure browser and advanced modes with Alibaba Cloud CLI, then select the
same profile in `ecctl`:

```bash
aliyun configure --mode OAuth --profile production
ecctl --profile production ecs instance list --region cn-hangzhou
```

`ecctl` keeps the selected credential provider for the complete command and
refreshes temporary credentials before later signed requests when needed. The
first renewable credential pins the canonical account, user, or role; a later
credential for another identity is rejected before it can sign a request. It
does not implement a second OAuth or CloudSSO browser-login flow. If
interactive authentication has expired, authenticate that profile again with
Alibaba Cloud CLI. Changing the selected profile's identity fields while a
command is running fails closed instead of switching accounts mid-command.
RAM role and OIDC profiles must use a complete
`acs:ram::<16-digit-account-id>:role/<role-name>` ARN. `ecctl` derives the
expected account from that ARN and verifies the initial credential through an
official STS `GetCallerIdentity` endpoint before the first business request.
An explicit custom `sts_endpoint` may issue credentials, but it is never
trusted to verify its own result. Set `sts_region` and `enable_vpc` when the
independent identity check must use a regional or VPC STS endpoint.

Cloud commands treat the compatible `aliyun` configuration as read-only.
Rotated OAuth tokens and cached OAuth/CloudSSO STS credentials are stored as
per-profile entries under `~/.ecctl/credentials-v2/` with current-user-only
permissions. The store uses the home directory resolved for the current
process and does not move with `ECCTL_CONFIG_PATH`, so two ecctl config files
that select the same resolved Aliyun config path and profile share one OAuth
rotation owner. Entry names are hashes of that resolved source path and profile
name. Removing an entry never modifies the source profile. If a server-side
refresh-token rotation cannot be committed locally, `ecctl` stops without
retrying the rotation and the profile must be authenticated again.

For OSS commands backed by a renewable credential, `ecctl` gives the local
`ossutil` child access through a short-lived credential endpoint bound only to
`127.0.0.1` and a temporary profile readable only by the current user. The
endpoint uses an unguessable per-command path; both it and the profile are
removed when the child exits, and credentials never appear in command
arguments. External credential acquisition has a 60-second deadline. Unix
process groups are terminated on cancellation; on every platform inherited
output pipes are forcibly released after a further two-second grace period.
An External AK without an expiration is passed to OSS as an operation-static
AK; renewable OSS broker responses must be STS credentials with a security
token.

The upstream Dara request logger prints signed URLs and headers before
`ecctl`'s final HTTP client can redact them. Credential-bearing commands
therefore fail closed when the comma-separated `DEBUG` environment variable
contains the exact token `dara`. Remove that token before retrying.

For a local AK profile managed by `ecctl`:

```bash
ecctl configure set access-key-id <id>
ecctl configure set access-key-secret <secret>
```

For STS access, also set the security token:

```bash
ecctl configure set security-token <token>
```

`StsToken` credentials are used as-is and cannot refresh themselves. When the
profile contains `sts_expiration`, `ecctl` rejects an expired token and rejects
a token that cannot cover a known command deadline. For long OSS transfers,
prefer a renewable mode such as OAuth, OIDC, a RAM role, an ECS role, External,
or CredentialsURI.

`External` and `CredentialsURI` may execute a local program or contact an
external endpoint. Set `ALIBABA_CLOUD_DISABLE_EXTERNAL_PROCESS=true` to disable
both sources. External commands are parsed into argv and executed directly;
they are never evaluated through a shell. CredentialsURI requires HTTPS unless
the URL uses a literal loopback IP address such as `127.0.0.1` or `::1`.

A CredentialsURI endpoint follows the Alibaba Cloud CLI response contract: it
must return HTTP 200 and JSON containing `Code: "Success"`, `AccessKeyId`,
`AccessKeySecret`, `SecurityToken`, and `Expiration` in RFC 3339 UTC format.

## Supported Keys

List supported keys:

```bash
ecctl configure list
```

Current keys:

| Key | Stored as | Allowed values |
|---|---|---|
| `region` | `region_id` | Any syntactically valid Alibaba Cloud region ID |
| `access-key-id` | `access_key_id` | String |
| `access-key-secret` | `access_key_secret` | String, sensitive |
| `security-token` | `sts_token` | String, sensitive |
| `lang` | `language` | `en`, `zh-CN` |
| `output` | `output_format` | `json`, `text` |

Secrets are masked by default. Use `--show-secret` only when you deliberately
need to inspect a local secret value.

## Profiles

Use `--profile` to write a named profile:

```bash
ecctl --profile production configure set output json
```

Switch the active profile after it exists:

```bash
ecctl configure use production
```

`configure use` checks both compatible `aliyun` configuration and `ecctl`
configuration for the profile name, then records the selected profile in the
`ecctl` config file.

Credential profile selection uses this order:

1. `--profile`
2. `ECCTL_PROFILE`, then compatible Alibaba Cloud profile environment variables
3. the active profile in local configuration

The selected profile wins over ordinary credential environment variables.
Set `ALIBABA_CLOUD_IGNORE_PROFILE=TRUE` to ignore stored credentials and use
only environment-provided credentials for the command. An explicitly selected
missing profile fails instead of silently switching identity.

## Global Overrides

Global flags override configuration for one command:

```bash
ecctl --region cn-beijing --output json --lang en schema --list ecs
```

Common global flags:

| Flag | Purpose |
|---|---|
| `--profile` | Select a configuration profile |
| `--region` | Select the Alibaba Cloud region for the current command |
| `--output` | Select `json` or `text` output |
| `--json` | Force JSON output |
| `--lang` | Select `en` or `zh-CN` user-facing text |
| `--no-color` | Disable color in human-readable output |
| `--agent-envelope` | Wrap JSON output in the ecctl Agent envelope |

## Environment Variables

`ecctl` recognizes these environment overrides:

| Variable | Purpose |
|---|---|
| `ECCTL_PROFILE`, `ALIBABACLOUD_PROFILE`, `ALIBABA_CLOUD_PROFILE`, `ALICLOUD_PROFILE` | Default profile when `--profile` is not passed |
| `ECCTL_REGION`, `ALIBABA_CLOUD_REGION_ID`, `ALIBABACLOUD_REGION_ID`, `ALICLOUD_REGION_ID` | Region override when `--region` is not passed |
| `ALIBABA_CLOUD_CONFIG_PATH`, `ALIBABACLOUD_CONFIG_PATH`, `ALICLOUD_CONFIG_PATH` | Path to a compatible `aliyun` CLI configuration file |
| `ALIBABA_CLOUD_IGNORE_PROFILE` | Set to `TRUE` to ignore stored credential profiles |
| `ALIBABA_CLOUD_ACCESS_KEY_ID`, `ALIBABA_CLOUD_ACCESS_KEY_SECRET`, `ALIBABA_CLOUD_SECURITY_TOKEN` | AK or STS credentials |
| `ALIBABA_CLOUD_ROLE_ARN`, `ALIBABA_CLOUD_ROLE_SESSION_NAME`, `ALIBABA_CLOUD_EXTERNAL_ID` | RAM role assumption |
| `ALIBABA_CLOUD_ECS_METADATA`, `ALIBABA_CLOUD_IMDSV1_DISABLED` | ECS instance RAM role and IMDS policy |
| `ALIBABA_CLOUD_OIDC_PROVIDER_ARN`, `ALIBABA_CLOUD_OIDC_TOKEN_FILE` | OIDC/RRSA credentials |
| `ALIBABA_CLOUD_CREDENTIALS_URI` | CredentialsURI endpoint |
| `ALIBABA_CLOUD_BEARER_TOKEN`, `ALIBABA_CLOUD_BEARER_TOKEN_HEADER_KEY` | Bearer token and optional custom header |
| `ALIBABA_CLOUD_DISABLE_EXTERNAL_PROCESS` | Disable `External` and `CredentialsURI` credential sources |

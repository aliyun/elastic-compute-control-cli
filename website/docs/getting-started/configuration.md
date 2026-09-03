---
title: Configuration
description: Configure profiles, credentials, region, language, and output.
---

# Configuration

`ecctl configure` writes ecctl-local settings for resource commands. Native
OAuth login stores only non-secret profile metadata, including the verified
account ID, there. Access tokens, refresh tokens, and exchanged STS credentials
stay in the canonical private store under `~/.ecctl/credentials-v2/`.
For normal cloud commands, `ecctl` also reads compatible local `aliyun` CLI
profiles as a read-only fallback.

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

`ecctl` accepts the same eleven credential modes as Alibaba Cloud CLI, and it
reads compatible `~/.aliyun/config.json` profiles as a read-only fallback. Only
`OAuth` has an interactive `ecctl` setup flow:

```bash
ecctl configure --mode OAuth --profile production
ecctl --profile production ecs instance list --region cn-hangzhou
```

Every other mode is provisioned with `aliyun configure --mode <Mode>`, with
environment variables, or by writing a profile into the compatible
configuration file. A local AK or STS credential can also be set directly:

```bash
ecctl configure set access-key-id <id>
ecctl configure set access-key-secret <secret>
ecctl configure set security-token <token>
```

[Credentials](../credentials/index.md) is the reference for this area: mode
selection, the two configuration files and how their capabilities differ,
profile and credential resolution order, identity pinning, verification, and
the `DEBUG=dara` fail-closed rule. Each mode has its own page, reached from
that overview or from the sidebar.

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
| `ECCTL_ALIYUN_CONFIG_PATH`, `ALIBABA_CLOUD_CONFIG_PATH`, `ALIBABACLOUD_CONFIG_PATH`, `ALICLOUD_CONFIG_PATH` | Path to a compatible `aliyun` CLI configuration file, checked in this order |
| `ALIBABA_CLOUD_IGNORE_PROFILE` | Set to `TRUE` to ignore stored credential profiles |
| `ALIBABA_CLOUD_ACCESS_KEY_ID`, `ALIBABA_CLOUD_ACCESS_KEY_SECRET`, `ALIBABA_CLOUD_SECURITY_TOKEN` | AK or STS credentials |
| `ALIBABA_CLOUD_ROLE_ARN`, `ALIBABA_CLOUD_ROLE_SESSION_NAME`, `ALIBABA_CLOUD_EXTERNAL_ID` | RAM role assumption |
| `ALIBABA_CLOUD_ECS_METADATA`, `ALIBABA_CLOUD_IMDSV1_DISABLED` | ECS instance RAM role and IMDS policy |
| `ALIBABA_CLOUD_OIDC_PROVIDER_ARN`, `ALIBABA_CLOUD_OIDC_TOKEN_FILE` | OIDC/RRSA credentials |
| `ALIBABA_CLOUD_CREDENTIALS_URI` | CredentialsURI endpoint |
| `ALIBABA_CLOUD_BEARER_TOKEN`, `ALIBABA_CLOUD_BEARER_TOKEN_HEADER_KEY` | Bearer token and optional custom header |
| `ALIBABA_CLOUD_DISABLE_EXTERNAL_PROCESS` | Disable `External` and `CredentialsURI` credential sources |

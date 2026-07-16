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

Set AccessKey credentials:

```bash
ecctl configure set access-key-id <id>
ecctl configure set access-key-secret <secret>
```

For STS access, also set the security token:

```bash
ecctl configure set security-token <token>
```

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

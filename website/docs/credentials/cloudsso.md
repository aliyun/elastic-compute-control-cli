---
title: CloudSSO
description: Use an Alibaba Cloud CloudSSO access configuration with ecctl.
---

# CloudSSO

`CloudSSO` uses an enterprise CloudSSO access configuration to obtain temporary
credentials for a member account. The user signs in through the organization's
identity provider once; `ecctl` then exchanges the resulting access token for
STS credentials scoped to the selected account and access configuration.

Use this mode when your organization manages Alibaba Cloud access through
CloudSSO rather than through individual RAM users or long-lived access keys.

`ecctl` consumes CloudSSO profiles but does not run the interactive CloudSSO
login. Configure the profile with Alibaba Cloud CLI, then use it from `ecctl`.

## Configure with Alibaba Cloud CLI

```bash
aliyun configure --mode CloudSSO --profile sso
```

The interactive flow opens a browser, has you select the member account and
access configuration, and writes the profile. `ecctl configure --mode CloudSSO`
is not supported; `--mode` accepts `OAuth` only.

For native browser-backed login with `ecctl` itself, use
[OAuth](./oauth.md).

## Profile fields

| Field | Required | Notes |
|---|---|---|
| `mode` | Yes in practice | `CloudSSO`. Inferred when `cloud_sso_sign_in_url` is present |
| `cloud_sso_sign_in_url` | Yes | The organization CloudSSO portal URL |
| `cloud_sso_account_id` | Yes | The member account the credential targets |
| `cloud_sso_access_config` | Yes | The access configuration name or ID |
| `access_token` | No | Cached CloudSSO access token |
| `cloud_sso_access_token_expire` | No | Unix timestamp in seconds for the cached token |

```json
{
  "name": "sso",
  "mode": "CloudSSO",
  "cloud_sso_sign_in_url": "https://signin-cn-hangzhou.alibabacloudsso.com/login/....htm",
  "cloud_sso_account_id": "1234567890123456",
  "cloud_sso_access_config": "AdministratorAccess",
  "region_id": "cn-hangzhou"
}
```

`cloud_sso_account_id` and `cloud_sso_access_config` are both required. A
profile missing either fails during resolution rather than falling back to an
unscoped credential.

`access_token` and `cloud_sso_access_token_expire` are cache fields written by
the login flow. When the cached token is absent or its expiry has passed,
`ecctl` starts a fresh sign-in.

## Where renewable state lives

Cloud commands treat the compatible `aliyun` configuration as read-only.
Rotated CloudSSO tokens and cached CloudSSO STS credentials are persisted only
in the canonical per-user ecctl credential store under
`~/.ecctl/credentials-v2/`, with current-user-only permissions. Entries are
keyed by their resolved Aliyun source path and profile, so two different
configuration files holding the same profile name do not share an entry.

`ECCTL_CONFIG_PATH` does not create a second rotation owner for a CloudSSO
profile, and the private store does not move with `ECCTL_CONFIG_PATH`.

## Identity pinning

`cloud_sso_account_id` is treated as the expected account for the profile. A
refreshed credential for a different account is rejected before it can sign a
request, so a changed CloudSSO assignment or a re-authentication into a
different member account fails closed instead of switching accounts
mid-command.

Treat identity, target, or auth-field changes during credential refresh as
fatal conflicts. Only a write-stage cache persistence failure may degrade to an
in-memory credential for the remainder of the command.

## Verify

```bash
ecctl --profile sso configure get
ecctl --profile sso --region cn-hangzhou ecs region list
```

`configure get` reports the declared profile without a cloud call. The second
command exercises the real path: token validity, account and access
configuration scoping, and STS exchange.

When the cached token has expired and no browser session is available, the
command fails rather than silently using a stale credential. Re-run
`aliyun configure --mode CloudSSO --profile sso`.

## Related

- [OAuth](./oauth.md) for browser login managed natively by `ecctl`
- [Credentials overview](./index.md) for resolution order and the private store

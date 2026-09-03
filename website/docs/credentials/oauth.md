---
title: OAuth
description: Authenticate ecctl with a browser-based Alibaba Cloud OAuth login.
---

# OAuth

`OAuth` is the recommended mode for a person working at a terminal. It
exchanges a browser login for a refresh token, then mints short-lived STS
credentials from that token as needed. No long-lived secret is stored on disk
in the ecctl configuration file, and `ecctl` is the only credential mode with a
native interactive setup flow.

`OAuth` is renewable, so it is suitable for long-running operations such as
large OSS transfers.

## Log in

```bash
ecctl configure --mode OAuth --profile production
```

The command opens a browser, waits for the authorization callback, verifies the
account, and writes the profile. Successful stdout contains only the profile,
mode, site, verified account ID, config path, and browser-launch status. It
never contains tokens.

Use the result directly:

```bash
ecctl --profile production ecs instance list --region cn-hangzhou
```

## Site type

The default site is `CN`. Select the international site when the account lives
there:

```bash
ecctl configure --mode OAuth --profile production --oauth-site-type INTL
```

| `--oauth-site-type` | OAuth endpoint | Sign-in endpoint |
|---|---|---|
| `CN` | `https://oauth.aliyun.com` | `https://signin.aliyun.com` |
| `INTL` | `https://oauth.alibabacloud.com` | `https://signin.alibabacloud.com` |

Any other value fails with `OAuth site type must be CN or INTL`.

## Account binding

A login is bound to one Alibaba Cloud account. The first interactive login
displays the verified account ID and asks you to type the complete value before
credentials are stored. A later login must match the account already recorded
for that profile.

In a non-interactive terminal, or when the account is known in advance, supply
it explicitly:

```bash
ecctl configure --mode OAuth --profile production --expected-account-id 1234567890123456
```

Supplying a different `--expected-account-id` is the explicit way to authorize
an intentional account change. Without it, a login for a different account is
rejected rather than silently replacing the recorded identity.

## Manual browser launch

The login uses PKCE and an HTTP callback bound only to `127.0.0.1` on ports
12345 through 12349. A successful automatic browser launch does not print the
one-time authorization URL.

When the URL must be opened manually, use `--manual` in a private terminal:

```bash
ecctl configure --mode OAuth --profile production --manual
```

Do not copy the one-time URL into shared logs, issue trackers, or chat. It is a
bearer of the pending authorization. Browser launcher processes do not inherit
Alibaba Cloud or OSS credential environment variables.

## Custom metadata path

Select an explicit ecctl metadata file when the default location is unsuitable:

```bash
ecctl configure --mode OAuth --profile production --config-path /path/to/config.json
```

For later resource commands that should use the same file, export
`ECCTL_CONFIG_PATH=/path/to/config.json` and select the same profile. A native
OAuth config path must not be the Aliyun CLI config path.

The private credential store does not move with `ECCTL_CONFIG_PATH`. It stays
under `~/.ecctl/credentials-v2/` resolved from the current process home
directory.

## What is stored where

| Location | Contents |
|---|---|
| `~/.ecctl/config.json` | Non-secret metadata: `mode`, `oauth_site_type`, `oauth_generation`, `oauth_account_id`, plus region, language, and output |
| `~/.ecctl/credentials-v2/` | Access tokens, refresh tokens, and exchanged STS credentials, with current-user-only permissions |

Native OAuth entries have one per-user owner per profile. A changed login
generation invalidates older metadata instead of switching identity. Tokens are
never written into the Aliyun configuration file.

Native OAuth cache writes compare the active generation under a per-profile
lock. Login also keeps a private write-ahead transaction until the cache and
the ecctl metadata agree. If the process or host stops between those writes,
the next login or credential load restores the previous generation or completes
the new one before using any token. If a server-side refresh-token rotation
cannot be committed locally, `ecctl` stops without retrying the rotation and
the profile must be authenticated again.

## Verify

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

## Reauthentication

When native OAuth authentication has expired, run the login again:

```bash
ecctl configure --mode OAuth --profile production
```

An OAuth profile that originated in a compatible `aliyun` configuration is
reauthenticated with `aliyun configure`, not with `ecctl`.

## Consuming an existing OAuth profile

`ecctl` also reads an OAuth profile from a compatible `aliyun` configuration
file. Such a profile carries `oauth_site_type`, `oauth_refresh_token`,
`oauth_refresh_token_expire`, `oauth_access_token`, and
`oauth_access_token_expire`. Rotated tokens and cached STS credentials for
those profiles are stored as per-profile entries under
`~/.ecctl/credentials-v2/`, keyed by their resolved source path and profile.
The compatible `aliyun` file itself is treated as read-only during cloud
commands.

Native ecctl OAuth metadata takes precedence over a same-name Aliyun profile.

## Related

- [Credentials overview](./index.md)
- [CloudSSO](./cloudsso.md) for browser-backed enterprise single sign-on
- [Configuration](../getting-started/configuration.md)

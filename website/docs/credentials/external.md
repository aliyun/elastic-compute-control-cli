---
title: External process
description: Run a local credential helper program to supply credentials to ecctl.
---

# External process

`External` runs a local program and reads credentials from its stdout. Use it
to bridge `ecctl` to an existing secret manager, a corporate credential broker,
or a vault agent, without storing anything in a configuration file.

The helper is executed directly from a parsed argv. It is never evaluated
through a shell, so shell metacharacters in `process_command` are not
interpreted.

## Configure with Alibaba Cloud CLI

```bash
aliyun configure --mode External --profile vault
```

`ecctl configure --mode External` is not supported. `--mode` accepts `OAuth`
only, and an ecctl-native profile resolves only OAuth or a static credential.
A profile in the ecctl configuration file that declares `External` without
static credentials fails with `MissingCredentials`. Put this profile in the
Aliyun-compatible configuration file.

## Profile fields

| Field | Required | Notes |
|---|---|---|
| `mode` | No | `External`. Inferred when `process_command` is present |
| `process_command` | Yes | Command line, parsed into argv |

```json
{
  "name": "vault",
  "mode": "External",
  "process_command": "/usr/local/bin/vault-aliyun-credential --role ecctl",
  "region_id": "cn-hangzhou"
}
```

A profile with no `process_command` fails with `process_command is required for
External credentials`. The separate message `process_command is empty` means the
field is present but tokenized to zero arguments, which happens for a value made
entirely of quotes or whitespace.

### Quoting

The command is tokenized with quote awareness. Single and double quotes group
arguments, so a path containing spaces works:

```json
{"process_command": "\"/opt/my tools/get-credential\" --profile 'team a'"}
```

Windows profiles are tokenized with Windows rules. Because nothing goes through
a shell, environment variable expansion, pipes, redirection, and command
substitution inside `process_command` do not happen. Resolve those in the
helper itself.

## Output contract

The helper must print a single JSON object to stdout:

| Field | Required | Notes |
|---|---|---|
| `mode` | Yes | `AK` or `StsToken` only |
| `access_key_id` | Yes | |
| `access_key_secret` | Yes | |
| `sts_token` | Required for `StsToken` | Security token |
| `expiration` | No | RFC 3339 UTC. Must be in the future when present |

```json
{
  "mode": "StsToken",
  "access_key_id": "STS.NUgYrLnoC...",
  "access_key_secret": "...",
  "sts_token": "...",
  "expiration": "2026-09-03T12:00:00Z"
}
```

Any other `mode` is rejected:

```json
{
  "error": {
    "kind": "client",
    "code": "InvalidCredentials",
    "message": "external credential command returned an unsupported mode"
  }
}
```

This means an External helper cannot itself return an OAuth, OIDC, or role
credential. It returns a usable key pair, optionally temporary.

Missing required fields produce `external credential command returned
incomplete credentials`. A malformed, absent, or past `expiration` produces
`external credential command returned an invalid expiration` or `external
credential command returned expired credentials`. Invalid JSON produces
`external credential command returned invalid JSON`.

Omitting `expiration` is allowed for `mode: AK`, and the key is passed to the
operation as a static credential that never expires from `ecctl`'s point of
view.

## Execution limits

| Limit | Value |
|---|---|
| Acquisition deadline | 60 seconds |
| Captured stdout | 1 MiB |
| Post-cancellation grace | 2 seconds |

Output beyond 1 MiB fails with `external credential output exceeds size limit`.
A helper that exceeds the deadline is treated as a failed acquisition.

Any failure inside the helper is reported as `external credential command
failed`. The helper's own stderr and exit code are not echoed into the `ecctl`
error, so log them from the helper when you need to diagnose a failure.

On Unix, the helper's process group is terminated on cancellation. On every
platform, inherited output pipes are forcibly released after the two-second
grace period, so a helper that spawns a long-lived child cannot hold the
command open.

## Disabling this source

Because `External` executes a local program, it can be turned off wholesale:

```bash
export ALIBABA_CLOUD_DISABLE_EXTERNAL_PROCESS=true
```

```json
{
  "error": {
    "kind": "client",
    "code": "CredentialSourceDisabled",
    "message": "ALIBABA_CLOUD_DISABLE_EXTERNAL_PROCESS disables External credentials"
  }
}
```

The variable accepts `1` or `true`, case-insensitively. It disables
`CredentialsURI` as well. Set it in environments where a configuration file
might be influenced by someone else, so an injected `process_command` cannot
run.

## Verify

```bash
ecctl --profile vault configure get
ecctl --profile vault --region cn-hangzhou ecs region list
```

Run the helper directly first and confirm its stdout is exactly one JSON
object with no log preamble:

```bash
/usr/local/bin/vault-aliyun-credential --role ecctl
```

Anything printed before the JSON makes the output unparseable. Send diagnostics
to stderr.

## Renewal

Whether the credential renews depends on `expiration`. With a future
`expiration`, `ecctl` re-runs the helper before a later signed request as the
credential approaches expiry. Without one, the credential is static for the
life of the command.

The first renewable credential pins the canonical identity. A later invocation
that returns a different identity is rejected before it can sign a request, so
a helper whose backend switched accounts fails closed instead of changing
identity mid-command.

## OSS transfers

For OSS commands backed by a renewable credential, `ecctl` gives the local
`ossutil` child access through a short-lived credential endpoint bound only to
`127.0.0.1` and a temporary profile readable only by the current user. The
endpoint uses an unguessable per-command path; both it and the profile are
removed when the child exits, and credentials never appear in command
arguments. An External AK without an expiration is passed to OSS as an
operation-static AK; renewable OSS broker responses must be STS credentials
with a security token.

## Related

- [Credentials URI](./credentials-uri.md) for the HTTP equivalent
- [Credentials overview](./index.md)

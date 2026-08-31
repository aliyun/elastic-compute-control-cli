# `@aliyun/dsh-ecctl`

A [DeepSeek Harness](https://github.com/deepseek-ai/deepseek-harness) (DSH)
Cordis plugin that exposes the public resource surface of
[`ecctl`](https://github.com/aliyun/elastic-compute-control-cli) as four typed,
model-facing tools.

## Tools

| Tool | What it does | Canonical result |
|---|---|---|
| `ecctl_version` | Confirm the configured `ecctl` binary and version. | String |
| `ecctl_capabilities` | Discover the public product/resource/action catalog. | Parsed JSON |
| `ecctl_schema` | Inspect required parameters, risk, dry-run, idempotency, and waiter behavior. | Parsed JSON |
| `ecctl_run` | Run one capabilities-listed public resource operation after one-shot approval. | Parsed JSON |

The intended loop is **discover → schema → approved run**. Nonzero exits,
spawn failures, timeouts, buffer overflows, and invalid JSON are failed DSH tool
outcomes rather than successful strings.

## Install

The plugin must live in the selected DSH profile's module tree so the loader can
resolve it by package name. From this repository checkout:

```bash
dsh plugin --profile web add file:"$(pwd)/dsh-plugin"
```

Or, once the package is published:

```bash
dsh plugin --profile web add @aliyun/dsh-ecctl
```

`web` and `headless` profiles initialize on first use. Other profiles are
created by the first `dsh plugin --profile <name> ...` command.

## Add the Cordis row

A tool plugin belongs on the agent-preset plane in the Web surface. Add the row
to the selected preset's `agent.cordis.yml`:

```yaml
- id: ecctl
  name: '@aliyun/dsh-ecctl'
  config:
    bin: ecctl
```

For a process-global smoke test, the same row can be inserted into the profile's
`cordis.patch.yml`. Restart the profile and inspect the tools registry to confirm
that `ecctl_version`, `ecctl_capabilities`, `ecctl_schema`, and `ecctl_run` are
present.

## `ecctl_run` contract

```text
command         canonical public resource-operation tokens plus positional IDs
region          explicit Alibaba Cloud region when the operation requires one
config_profile  ecctl credential/config profile
lang            en or zh-CN
filters         repeated key=value filters
tags            repeated key=value tags
dry_run         only accepted when the operation schema supports dry-run
extra_args      resource-specific inline arguments
```

`command` is matched against the installed binary's `ecctl capabilities`
catalog. Auxiliary commands such as `configure`, raw `call`, `update`, and
completion are therefore not executable through this tool. The plugin also:

- requires a one-shot DSH approval for every `ecctl_run`; missing approval
  support or an absent agent turn fails closed;
- places `config_profile` before the resource command, avoiding collisions with
  resource-local flags such as ACK create's `--profile`;
- rejects attempts to override JSON, region, language, dry-run, filter, tag,
  raw API parameters, or idempotency flags through `extra_args`;
- rejects `@file` values because the plugin runs in the host process rather than
  the DSH filesystem sandbox;
- derives a stable idempotency key from the agent/session identity, DSH call
  identity, and complete canonical request when the operation schema advertises
  idempotency support;
- supplies approval answerers with a JSON-escaped target and behavior-flag
  summary without copying argument values that may contain secrets;
- labels failed or interrupted mutation outcomes as unknown and tells callers to
  reconcile state before starting a new invocation.

## Config

| Field | Default | Meaning |
|---|---:|---|
| `bin` | `ecctl` | Executable name or path. |
| `timeoutMs` | `3900000` | Hard child-process timeout (65 minutes). |
| `maxOutputBytes` | `16777216` | Maximum captured bytes for each of stdout and stderr. |

The DSH execution signal is forwarded to `execFile`, so cancellation waits for
the child to stop before the tool settles. `ECCTL_DISABLE_UPDATE_CHECK=1` is set
for every child.

## Trust model

This remains a trusted, out-of-tree plugin running inside the DSH host process.
It inherits the host's environment and launches `ecctl` with the selected
credentials. The public-operation allowlist, host-file denial, typed flag
ownership, one-shot approval, and cancellation forwarding narrow that boundary;
they do not turn the plugin into a sandbox. Install it only in profiles whose
agent, approval answerer, and credential scope you trust.

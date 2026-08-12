<p align="center">
  <img src="docs/assets/logo.png" alt="ecctl" width="160">
</p>
<p align="center">
  <em>Agent-first command-line control for Alibaba Cloud elastic computing resources.</em>
</p>
<p align="center">
  <a href="https://github.com/aliyun/elastic-compute-control-cli/actions/workflows/ci.yml"><img src="https://github.com/aliyun/elastic-compute-control-cli/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="https://aliyun.github.io/elastic-compute-control-cli/"><img src="https://img.shields.io/badge/docs-online-3d8bfd" alt="Documentation"></a>
  <img src="https://img.shields.io/badge/Go-1.25%2B-00ADD8?logo=go&logoColor=white" alt="Go 1.25 or later">
</p>
<p align="center">
  <strong>Documentation</strong>: <a href="https://aliyun.github.io/elastic-compute-control-cli/">English</a> | <a href="https://aliyun.github.io/elastic-compute-control-cli/zh-Hans/">简体中文</a>
</p>

`ecctl` gives agents and operators a consistent product/resource/action grammar,
JSON-first output, machine-readable schemas, waiters, and spec-driven cloud
behavior.

## Installation

Install the latest public release with Homebrew:

```bash
brew tap aliyun/ecctl https://github.com/aliyun/elastic-compute-control-cli
brew install ecctl
ecctl --version
```

The first command uses this repository directly as the `aliyun/ecctl` Tap. Pre-built
binaries for macOS, Linux, and Windows are also available from
[GitHub Releases](https://github.com/aliyun/elastic-compute-control-cli/releases).

Check for or install a newer release with `ecctl update --check` and
`ecctl update`. The command supports both direct binary and Homebrew
installations.

Or install with Go 1.25 or later:

```bash
go install github.com/aliyun/elastic-compute-control-cli/cmd/ecctl@latest
```

See the [installation guide](https://aliyun.github.io/elastic-compute-control-cli/docs/getting-started/installation) for requirements and other installation options.

## Usage

Inspect the command surface before running cloud operations:

```bash
ecctl schema --list
ecctl schema --list ecs
ecctl schema ecs.instance.create --brief
```

Run resource commands with the regular product/resource/action shape:

```bash
ecctl vpc list
ecctl ecs instance list --filter status=Running
```

Learn more in the [Quick Start](https://aliyun.github.io/elastic-compute-control-cli/docs/getting-started/quick-start), [Concepts](https://aliyun.github.io/elastic-compute-control-cli/docs/user-guide/concepts), [Command Discovery](https://aliyun.github.io/elastic-compute-control-cli/docs/user-guide/discovery), and [Resource Coverage](https://aliyun.github.io/elastic-compute-control-cli/docs/reference/resource-coverage) guides.

## Telemetry

Official release binaries send best-effort OpenTelemetry traces directly to
Alibaba Cloud ARMS/SLS to measure command executions, Alibaba Cloud API request
attempts, pseudonymous active cloud identities, and pseudonymous active
installations. An installation is counted only after that installation runs
ecctl during the selected reporting period; it is not a download count. ecctl
never includes command arguments, resource IDs, regions, profile names, host
names, AccessKeys, RequestIds, or error messages. Identity values are one-way
SHA-256 hashes of the stable account, RAM user, or assumed-role identifier
returned by STS.
Installation values are one-way SHA-256 hashes derived from a locally generated
random token and cannot be used to recover machine or user identifiers.
Installation persistence is disabled on Windows because ecctl does not create a
private Windows DACL for this state; Windows commands therefore omit the
installation hash.

Disable telemetry globally with:

```bash
ecctl configure set telemetry.enabled false
```

`ECCTL_DISABLE_TELEMETRY=1` and `DO_NOT_TRACK=1` also disable it. Source builds,
test builds, and full-surface development builds do not contain a telemetry
destination. Because the reporting endpoint is embedded in public release
binaries, this data is forgeable best-effort product analytics and must not be
used for billing, security auditing, or other trusted decisions.

## Contributing

Start with the [resource spec guide](https://aliyun.github.io/elastic-compute-control-cli/docs/contributing/resource-specs).

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
brew tap aliyun/ecctl https://github.com/aliyun/ecctl
brew install ecctl
ecctl --help
```

Or install with Go 1.25 or later:

```bash
go install github.com/aliyun/ecctl/cmd/ecctl@latest
```

Pre-built binaries are also available from [GitHub Releases](https://github.com/aliyun/ecctl/releases).

Build from a source checkout:

```bash
git clone https://github.com/aliyun/elastic-compute-control-cli.git
cd elastic-compute-control-cli
make build
./bin/ecctl --help
```

See the [installation guide](https://aliyun.github.io/elastic-compute-control-cli/docs/getting-started/installation) for requirements and other installation options.

## Usage

Inspect the command surface before running cloud operations:

```bash
./bin/ecctl schema --list
./bin/ecctl schema --list ecs
./bin/ecctl schema ecs.instance.create --brief
```

Run resource commands with the regular product/resource/action shape:

```bash
./bin/ecctl vpc list
./bin/ecctl ecs instance list --filter status=Running
```

Learn more in the [Quick Start](https://aliyun.github.io/elastic-compute-control-cli/docs/getting-started/quick-start), [Concepts](https://aliyun.github.io/elastic-compute-control-cli/docs/user-guide/concepts), [Command Discovery](https://aliyun.github.io/elastic-compute-control-cli/docs/user-guide/discovery), and [Resource Coverage](https://aliyun.github.io/elastic-compute-control-cli/docs/reference/resource-coverage) guides.

## Contributing

```bash
make test
make lint
```

Before changing resource behavior, read the online [resource spec guide](https://aliyun.github.io/elastic-compute-control-cli/docs/contributing/resource-specs).

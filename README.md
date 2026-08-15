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

## Common Commands

These commands are a curated set of common workflows for users and agents.
Unlike Alibaba Cloud CLI's API-operation-oriented interface, they demonstrate
ecctl's resource verbs, machine-readable command details, normalized filters,
concise inputs, multi-API workflows, built-in waiters, and readback. Replace
values in angle brackets before running a command. Several examples change
cloud resources; inspect their schema first.

### Check several commands before use

```bash
ecctl schema ecs.instance.list ecs.instance.create ecs.disk.create --brief
```

One call returns required flags, risk, dry-run, idempotency, and waiter behavior.

### Find running production instances

```bash
ecctl ecs instance list --region cn-hangzhou --filter status=Running --filter tag.env=prod
```

The consistent filter syntax returns normalized JSON without OpenAPI response wrappers.

### Validate ECS creation

```bash
ecctl ecs instance create --region cn-hangzhou --type <instance-type> --image <image-id-or-name> --sg <sg-id> --vswitch <vswitch-id> --tag env=prod --dry-run
```

Short resource fields send a server-side validation request without creating an instance.

### Create an ECS instance

```bash
ecctl ecs instance create --region cn-hangzhou --type <instance-type> --image <image-id-or-name> --sg <sg-id> --vswitch <vswitch-id> --name web-01 --tag env=prod
```

ecctl supplies ClientToken-compatible idempotency, waits for `Running`, and reads the instance back.

### Update and tag an instance

```bash
ecctl ecs instance update <instance-id> --region cn-hangzhou --name web-02 --tag env=prod
```

ecctl selects the required OpenAPIs from the requested resource changes and reads the instance back.

### Create a cloud disk

```bash
ecctl ecs disk create --region cn-hangzhou --zone <zone-id> --size 100 --category cloud_essd --name data-01 --tag env=prod
```

The command supplies idempotency, waits for `Available`, and returns the normalized disk view.

### Attach a key pair

```bash
ecctl ecs instance update <instance-id> --region cn-hangzhou --key-pair <key-pair-name>
```

The resource update maps to the required key-pair API and reads the instance back.

### Run a command on an instance

```bash
ecctl ecs instance exec <instance-id> --region cn-hangzhou --command 'uname -a'
```

One command runs Cloud Assistant, waits for completion, and reads the invocation result.

### Authorize an HTTPS security-group rule

```bash
ecctl ecs sg authorize <sg-id> --region cn-hangzhou --rule tcp:443@0.0.0.0/0
```

A compact rule replaces verbose OpenAPI fields, and ecctl reads the security group back.

### Get an ACK kubeconfig

```bash
ecctl ack kubeconfig get --region cn-hangzhou --cluster <cluster-id> --private-ip
```

The resource intent is explicit; callers do not need to know the OpenAPI operation or response shape.

Learn more in the [Quick Start](https://aliyun.github.io/elastic-compute-control-cli/docs/getting-started/quick-start), [Concepts](https://aliyun.github.io/elastic-compute-control-cli/docs/user-guide/concepts), [Command Discovery](https://aliyun.github.io/elastic-compute-control-cli/docs/user-guide/discovery), and [Resource Coverage](https://aliyun.github.io/elastic-compute-control-cli/docs/reference/resource-coverage) guides.

## Contributing

Start with the [resource spec guide](https://aliyun.github.io/elastic-compute-control-cli/docs/contributing/resource-specs).

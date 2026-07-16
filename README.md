<h1 style="display: flex; align-items: center; gap: 12px;">
  <img src="docs/assets/logo.png" alt="ecctl logo" width="32" height="32">
  <span>ecctl</span>
</h1>

`ecctl` is an Agent-first command-line controller for Alibaba Cloud elastic computing resources.

It gives agents and operators a regular resource/action grammar, JSON-first output, machine-readable schemas, waiters, and spec-driven cloud behavior.

> Documentation: [Online documentation](https://aliyun.github.io/elastic-compute-control-cli/) · [Documentation source](website/README.md)

## Installation

`ecctl` requires Go 1.25 or later.

Install the latest public release:

```bash
go install github.com/aliyun/ecctl/cmd/ecctl@latest
ecctl --help
```

Build from a source checkout:

```bash
make build
./bin/ecctl --help
```

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

For configuration, resource coverage, and full command examples, start with the
[online documentation](https://aliyun.github.io/elastic-compute-control-cli/),
or browse the source pages directly:

- [Overview](website/docs/intro.md)
- [Concepts](website/docs/user-guide/concepts.md)
- [Quick Start](website/docs/getting-started/quick-start.md)
- [Discovery](website/docs/user-guide/discovery.md)
- [Resource Coverage](website/docs/reference/resource-coverage.md)

## Documentation Development

Preview the documentation site locally:

```bash
cd website
npm install
npm run build
npm run serve
```

## Contributing

```bash
make test
make lint
```

See the design notes in [`docs/design/`](docs/design/) and the resource spec guide in [`website/docs/contributing/resource-specs.md`](website/docs/contributing/resource-specs.md) before changing resource behavior.

---
title: Installation
description: Install ecctl with Homebrew or Go, or build it from source.
---

# Installation

## Requirements

- Homebrew for the recommended installation method, or Go 1.25 or later.
- Alibaba Cloud credentials for commands that call cloud APIs.
- Optional: an existing compatible `aliyun` CLI configuration file.

## Install with Homebrew

Install the latest public release:

```bash
brew tap aliyun/ecctl https://github.com/aliyun/ecctl
brew install ecctl
```

## Install with go install

If Go 1.25 or later is installed locally, install the latest release with:

```bash
go install github.com/aliyun/ecctl/cmd/ecctl@latest
```

This places the `ecctl` binary in `$(go env GOPATH)/bin`. Make sure that
directory is on your `PATH`, then verify:

```bash
ecctl --version
ecctl --help
```

You can also download a pre-built archive from
[GitHub Releases](https://github.com/aliyun/ecctl/releases).

## Build from Source

To build from a checkout, run from the repository root:

```bash
make build
```

The binary is written to `bin/ecctl`.

Verify it:

```bash
./bin/ecctl --version
./bin/ecctl --help
```

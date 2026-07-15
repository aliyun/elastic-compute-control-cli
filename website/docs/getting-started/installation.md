---
title: Installation
description: Install ecctl with go install or build it from source.
---

# Installation

## Requirements

- Go 1.25 or later.
- Alibaba Cloud credentials for commands that call cloud APIs.
- Optional: an existing compatible `aliyun` CLI configuration file.

Check the local Go toolchain:

```bash
go version
```

## Install with go install

Install the latest release:

```bash
go install github.com/aliyun/ecctl/cmd/ecctl@latest
```

This places the `ecctl` binary in `$(go env GOPATH)/bin`. Make sure that
directory is on your `PATH`, then verify:

```bash
ecctl --version
ecctl --help
```

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

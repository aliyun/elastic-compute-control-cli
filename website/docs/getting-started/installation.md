---
title: Installation
description: Install ecctl with Homebrew, download a release, or build it from source.
---

# Installation

## Requirements

- Homebrew on macOS for the recommended installation method.
- Go 1.25 or later only when building from source.
- Alibaba Cloud credentials for commands that call cloud APIs.
- Optional: an existing compatible `aliyun` CLI configuration file.

## Install with Homebrew

Install the latest public release:

```bash
brew tap aliyun/ecctl https://github.com/aliyun/elastic-compute-control-cli
brew install ecctl
ecctl --version
```

The first command explicitly uses this repository as the `aliyun/ecctl` Tap. To
upgrade an existing installation:

```bash
brew update
brew upgrade ecctl
```

## Download a Pre-built Binary

Download the archive for your operating system and architecture from
[GitHub Releases](https://github.com/aliyun/elastic-compute-control-cli/releases),
then extract `ecctl` and place it on your `PATH`.

Verify the installation:

```bash
ecctl --version
ecctl --help
```

## Build from Source

Clone the repository and build from its root:

```bash
git clone https://github.com/aliyun/elastic-compute-control-cli.git
cd elastic-compute-control-cli
make build
```

The binary is written to `bin/ecctl`.

Verify it:

```bash
./bin/ecctl --version
./bin/ecctl --help
```

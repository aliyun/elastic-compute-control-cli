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
brew install --cask aliyun/ecctl/ecctl
ecctl --version
```

The first command explicitly uses this repository as the `aliyun/ecctl` Tap.
Since Homebrew 6.0, third-party taps are not trusted by default; installing
with the fully qualified cask name trusts only this cask, so a separate
`brew trust` step is not needed.

To upgrade an existing installation:

```bash
ecctl update
```

`ecctl update` supports both Homebrew and direct binary installations. You do
not need to run `brew update` first. See [Updates](../user-guide/updates.md) for
version checks, explicit versions, and automatic notifications.

## Download a Pre-built Binary

Download the archive for your operating system and architecture from
[GitHub Releases](https://github.com/aliyun/elastic-compute-control-cli/releases),
then extract `ecctl` and place it on your `PATH`.

Verify the installation:

```bash
ecctl --version
ecctl --help
```

## Enable Zsh Completion

Install completion for commands and flags:

```zsh
ecctl completion zsh
```

This saves the script to `~/.zfunc/_ecctl` and adds a loading block to
`~/.zshrc`. If `ZDOTDIR` is set, both files use that directory instead of your
home directory. Existing shell settings and permissions are preserved, and
running the command again updates the setup without appending duplicate blocks.
An empty `ZDOTDIR` is rejected; run `unset ZDOTDIR` to use your home directory.

The result includes the saved paths and a `reload_command`. Run that command
in your current terminal, or open a new zsh terminal. For the default paths:

```zsh
source ~/.zfunc/_ecctl
```

The script initializes zsh completion if needed. Use
`ecctl completion zsh --no-descriptions` to omit candidate descriptions.
`ecctl completion zsh` installs completion directly; it does not print a script
for redirection or process substitution.

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

---
title: Updates
description: Check for and install ecctl releases, including Homebrew installations.
---

# Updates

## Check and Install

Check the latest public version without changing the installation:

```bash
ecctl update --check
```

Install it:

```bash
ecctl update
```

Both commands return structured output showing the current version, target
version, whether an update is available, and whether installation completed or
is still pending.

Failures use stable update error codes and a `retryable` flag. The localized
`message` is suitable for display, while `detail` retains the diagnostic cause
for troubleshooting and automation.

To select a release explicitly, pass an unprefixed semantic version. A
downgrade or same-version reinstall requires `--force`:

```bash
ecctl update 0.2.0
ecctl update 0.2.0 --force
```

Homebrew installations can only select the latest stable release. Direct
binary installations can select an available historical or prerelease version.

## Validation and Installation

Before installation, ecctl validates the release metadata, checksums, and the
candidate executable. A validation failure stops the update without installing
untrusted or incomplete files.

For direct installations on macOS and Linux, ecctl replaces the executable only
after validation and restores the previous executable if post-installation
validation fails. An interrupted update is checked and recovered the next time
you explicitly run an update command.

On Windows, the running executable cannot be replaced in place. ecctl starts a
helper and returns `update_pending: true` with `updated: false`; replacement
continues after the update command exits. A later explicit `ecctl update`
reports any incomplete or failed replacement. Releases older than the first
self-updating Windows build must be installed manually.

## Homebrew Installations

When ecctl detects a supported Homebrew-managed installation, `ecctl update`
updates it through the matching Homebrew installation. You do not need to run
`brew update` first.

`--force` reinstalls the current stable version. If the Homebrew installation
cannot be identified safely, the update stops with an error instead of
overwriting a managed executable directly.

## Automatic Version Checks

Operational commands periodically check whether a newer stable version is
available. This advisory check never blocks the requested command. Notices are
written only to an interactive terminal on stderr, at most once per version per
day, so JSON stdout remains unchanged.

Skip automatic checks in controlled or offline environments:

```bash
export ECCTL_DISABLE_UPDATE_CHECK=1
```

Automatic checks also apply to help, version, completion, and update
invocations.

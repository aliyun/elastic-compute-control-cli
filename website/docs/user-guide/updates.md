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

For releases that publish the updater v2 protocol, OSS is the primary update
source. For the latest stable release, ecctl reads the OSS `version.txt`
pointer and then downloads that version's manifest and Sigstore bundle. For an
explicit version, ecctl goes directly to that version's manifest and does not
consult the latest-version pointer. The version directory and signed manifest
version identify an explicit release; no `/<version>/version.txt` object is
required.

The Sigstore bundle is verified locally against trust roots embedded in ecctl.
The accepted certificate is restricted to GitHub Actions' OIDC issuer and this
repository's `.github/workflows/release.yml` workflow on `main` or a matching
SemVer release tag. Verification
does not contact GitHub, Sigstore, Rekor, or another online service. Only after
the raw manifest signature and identity pass does ecctl parse the manifest,
validate `checksums.txt`, and verify the selected archive's SHA-256 digest and
size.

Availability and integrity failures are handled differently:

- If OSS is unreachable, returns a missing asset, or ends an otherwise valid
  response early, ecctl can use the compatible immutable GitHub Release path.
- An invalid bundle, unexpected signing identity, malformed manifest, or
  checksum, size, or archive mismatch is an integrity failure. ecctl stops and
  does not fall back to another source.

Historical releases without a v2 manifest continue to use immutable GitHub
Release metadata as the trust source while preferring OSS for matching asset
bytes. A validation failure always stops the update without installing
untrusted or incomplete files.

This protocol protects release authenticity and artifact integrity, not mirror
availability or absolute freshness. A mirror can still withhold a newer
release or keep serving the highest valid release a client has already seen.
ecctl detects a rollback below its previously verified stable-version
high-water mark, but it cannot prove that no unseen newer release exists.

For direct installations on macOS and Linux, ecctl replaces the executable only
after validation and restores the previous executable if post-installation
validation fails. An interrupted update is checked and recovered the next time
you explicitly run an update command.

On Windows, the running executable cannot be replaced in place. ecctl starts a
helper and returns `update_pending: true` with `updated: false`; replacement
continues after the update command exits. A later explicit `ecctl update`
reports any incomplete or failed replacement. Releases older than the first
self-updating Windows build must be installed manually.

Clients released before updater v2 cannot use the signed OSS-only path and may
still fail when GitHub's API is unavailable. Install or reinstall the first
ecctl release that contains updater v2 using the documented package, Homebrew,
or direct-download installation method. Very old updater v2 clients may also
need this bootstrap procedure after a future Sigstore trust-root rotation that
does not overlap their embedded roots.

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

Automatic checks use the same signed v2 resolution or immutable GitHub fallback
as an explicit update check. The cache stores only
`verified_latest_version` and never replaces it with a lower verified version.
The older unsigned `latest_version` cache field is ignored after upgrading.

Skip automatic checks in controlled or offline environments:

```bash
export ECCTL_DISABLE_UPDATE_CHECK=1
```

Automatic checks also apply to help, version, completion, and update
invocations.

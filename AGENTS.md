# Agent Guidelines

These instructions apply to contributors and AI coding agents working in this
repository.

## Project Layout

- `cmd/`: CLI entrypoints and developer tools.
- `pkg/`: shared implementation packages.
- `specs/`: resource specs and spec-specific hooks.
- `docs/design/`: public design notes and resource coverage.
- `pkg/spec/catalog_generated.go`: generated from `specs/`.

## Change Discipline

- Keep changes surgical and tied to the requested behavior.
- Prefer existing package patterns over new abstractions.
- Do not refactor adjacent code unless the change requires it.
- Remove only dead code introduced by your own change.

## Spec-Driven Cloud Behavior

Cloud resource behavior should be declared in `specs/*.yaml` first. Use Go hooks
only for cross-API derivation or normalization that the spec schema cannot
express.

After changing specs, run:

```bash
make generate
```

Commit the regenerated `pkg/spec/catalog_generated.go` when it changes.

## Localization

All user-facing translations and language-specific wording rules belong in
`pkg/i18n`. Callers should reference message IDs or template data instead of
hard-coding translated text or language branches.

Tests that assert exact user-facing text must explicitly select the intended
language through the product API, for example `--lang en`, rather than relying
on the host `LANG` or `LC_*` environment.

## Tests

Use the project targets first:

```bash
make test
make lint
```

Use `go test ./...` when validating the whole open-source tree. Avoid live cloud
API calls in default tests.

## Releases

- `version.txt` is the canonical public release version and must contain one
  unprefixed SemVer value followed by a newline.
- A release tag must be `v` followed by the exact `version.txt` value.
- Increasing `version.txt` on `main` starts the release workflow. Do not
  generate or rewrite it in CI, and never move an existing public tag.
- Keep GitHub release immutability and the `main`/`v*` repository rulesets
  enabled; they are part of the release authorization and integrity boundary.
- Treat Windows `__update` modes ending in `-v1` as a permanent cross-version
  protocol. Add new modes instead of removing or changing the v1 decoders.
- Keep every updater advisory-cache read and write under the same cache lock;
  Windows cannot atomically replace the cache while another process reads it.
- Treat the significant-line Homebrew Cask profile in
  `internal/releaseartifact` as the immutable updater Cask v1 protocol. A new
  Cask shape requires a versioned compatibility path while old clients still
  receive a v1-compatible asset.
- Keep the release platform/asset matrix synchronized across `.goreleaser.yaml`,
  `.github/scripts/validate-release.jq`, `pkg/updater`, and the OSS verification
  step in `.github/workflows/release.yml`; update their parity tests together.

## E2E Surface and Cleanup Gates

- Public E2E runs use `e2e/bin/ecctl-public` with `--surface public`; hidden
  cases use the separately built `e2e/bin/ecctl-full` and `--surface full`.
- Before a live run, run `make -C e2e lint`; completion uses the matching
  capability-filtered `coverage registry check --fail-on-not-live` gate.
- Cleanup journals are run-specific and may only replay validated `ecctl`
  delete commands with matching region, surface, and binary metadata.

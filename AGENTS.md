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

## Review Efficiency Contract

For repository-wide `/check` or equivalent reviews:

- Start in a fresh task with only the request, base/head refs, and repository
  instructions. Do not carry the implementation transcript into the review.
- Complete the initial review and collect the full blocker list before editing.
  Apply one repair batch instead of alternating between individual findings and
  broad rereviews.
- Give delegated reviewers a bounded scope plus compact diff context, not the
  full task history.
- Run at most one initial review and one final rereview for each review scope.
  If the final rereview finds a new blocker, stop and report it instead of
  recursively starting another broad review cycle.
- While repairing findings, run only the smallest relevant tests. Run
  `make review-final` exactly once for each final candidate SHA, from an
  isolated worktree based on the latest `origin/main`.
- Reuse the worktree-scoped Go and npm caches prepared by `make review-final`
  throughout a check. Set `REVIEW_CACHE_DIR` once when an explicit run-scoped
  location is needed; do not rotate cache directories between reviewers or
  verification commands.
- Limit live E2E for each changed or newly exposed operation to three rounds:
  reproduction, one diagnostic rerun when needed, and final public validation.

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

## Credential Configuration Safety

- Route every credential-config write through the shared resolved-target lock
  and atomic replacement implementation in `internal/configfile`.
- Preserve the existing target's permissions and security metadata for general
  configuration writes. Private credential-cache writes must instead use a
  canonical current-user-only policy. On Windows, both the cache directory and
  each cache entry use a protected DACL restricted to the current user and
  SYSTEM; never preserve an unvalidated DACL.
- Treat profile identity, target, or auth-field changes during credential
  refresh as fatal conflicts. Only a write-stage cache persistence failure may
  degrade to an in-memory CloudSSO credential.
- Validate derived STS endpoints before reading or transmitting OIDC tokens.
- Treat compatible `aliyun` configuration as read-only during cloud commands.
  Persist renewable OAuth and CloudSSO state only in the canonical per-user
  ecctl credential store. Key entries by resolved Aliyun source path and
  profile; `ECCTL_CONFIG_PATH` must not create another OAuth rotation owner.
- Native ecctl OAuth login stores only non-secret mode, site, login generation,
  and verified account metadata in the ecctl config. Store every token and STS
  result in the canonical private credential entry keyed independently of
  `ECCTL_CONFIG_PATH`; never write native OAuth tokens into the Aliyun config.
- Before interactive authorization, strictly validate the ecctl metadata file
  and reserve private credential persistence. Every native cache modification
  must compare the active generation under the canonical profile lock. Keep a
  private write-ahead transaction until cache and metadata generations agree,
  retain the previous entry until recovery is complete, and reconcile actual
  post-rename contents instead of inferring commit state from an error alone.
  Journal both the absolute requested config path and its resolved target, and
  durably retire private cache/journal paths before discarding recovery state:
  sync the containing directory on Unix and use a write-through tombstone move
  on Windows. Every successful atomic replacement must establish a
  platform-specific durability barrier: sync the parent directory on Unix; on
  Windows preserve the existing target metadata with `ReplaceFileW`, then open
  and flush the installed target with write-through semantics. Before retiring
  a successful transaction journal, establish that barrier and re-read both
  the resolved config generation and the desired cache mode/generation. Repeated
  tombstone sync failures remain post-commit uncertainty, and a tombstone that
  is the same file as its source must fail closed before rename.
  Reject profile deletion or same-profile authentication changes while
  allowing unrelated settings and concurrent current-profile switches to
  survive.
- Browser launchers must inherit a credential-scrubbed environment. Do not emit
  the one-time authorization URL after a successful automatic launch; manual
  URL output must be explicit or confined to an interactive terminal. Verify
  the OAuth account through trusted STS before persistence and require an
  expected account or explicit terminal confirmation for a first login.
- Pin the canonical account, user, or role for every renewable operation and
  reject a refreshed credential before use when its identity changes.
- Require HTTPS for CredentialsURI except for literal loopback HTTP endpoints,
  and require every CredentialsURI response to carry a valid future expiration.

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
- Before a live run, run `make -C e2e lint`.
- Offline review validates the entire matching capability surface without
  `--fail-on-not-live` and rejects invalid registry entries.
- Live completion is delta-based: every changed or newly exposed operation must
  be `live-pass`, and the branch may introduce no new `offline` or invalid
  entries relative to its base. Existing prerequisite-bound `offline` entries
  are not regressions when their registry records are unchanged.
- Use the matching capability-filtered
  `coverage registry check --fail-on-not-live` whole-surface gate only after all
  baseline offline exceptions have been cleared.
- Cleanup journals are run-specific and may only replay validated `ecctl`
  delete commands with matching region, surface, and binary metadata.

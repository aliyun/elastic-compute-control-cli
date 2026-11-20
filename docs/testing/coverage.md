# Go unit-test coverage

The root Go module has two independent coverage gates. Statement coverage must
be at least **95%**; branch-outcome coverage must be at least **85%**. Both compare
raw counts before rounding. Passing one does not satisfy the other.

Prepare the pinned metadata after checkout, then run:

```sh
make metadata-prepare
make coverage
make branch-coverage
```

CI runs the existing `make ci-test` statement gate, the branch tooling tests,
and `make branch-coverage`. The branch collector requires the existing Go and
Node toolchains and Git. It installs no global executable or service.

## Branch metric and tool version

The collector invokes **`github.com/rillig/gobco@v1.3.3` with `-branch`**.
The upstream module checksum is pinned in the collector as
`h1:I2qs2i0RvEKCS//lh8uCluFkXgSE/F2yjRXqC9IaUSE=`. Go verifies the downloaded
module; the collector also checks its checksum and reported version. The
collector copies that verified source, applies the versioned test-entry patch
`ecctl-gobco-subprocess-v1`, and builds a private executable for the run. The
module cache is read-only. The report records the patch ID and the before/after
SHA-256 of both patched templates, so the adapted tool is distinguishable from
unmodified upstream gobco.

The patch supports test binaries that re-execute themselves with an explicitly
scrubbed environment. When such a helper omits `GOBCO_STATS`, both generated and
existing `TestMain` entrypoints still run their tests and propagate their real
exit status, without attempting statistics-file I/O. This also lets helpers
exercise file-size and descriptor exhaustion without coverage setup failing
before the test body. Credential/environment scrubbing and resource-limit
assertions do not change. The regular parent process still loads and persists
statistics normally; missing/failed parent output still rejects collection.
Hits observed only in a scrubbed child are not merged into the parent report.
Their discovered branch outcomes remain in the denominator as uncovered unless
the parent also exercises them.

For each controlling decision in the tool's raw JSON, there are two outcomes:
`TrueCount > 0` and `FalseCount > 0`. The aggregate is:

```text
covered outcomes = sum of observed true outcomes and observed false outcomes
total outcomes   = 2 * number of instrumented decisions
branch coverage  = 100 * covered outcomes / total outcomes
```

Counts are summed across packages; package percentages are not averaged. The
false outcome of an `if` with an implicit `else` is included. An `a && b`
controlling expression is one decision. The instrumented expression retains
Go's short-circuit evaluation; this metric does not separately measure each
operand, MC/DC or complete execution paths. Gobco instruments `if`, conditional
`for`, and the comparisons it generates for expression and type switches. A
switch case contributes the comparison's true and false outcomes, rather than
one independently counted default arm.

The pin deliberately uses v1.3.3's syntax-based instrumentation. v1.3.4 adds a
source-import type checker before filtering platform files, introducing a
compatibility risk for this module's dependencies and platform implementations. A real
fixture verifies the selected version's decisions, black-box `TestMain`, named
boolean conditions, implicit false outcome and short-circuit behavior. Any
instrumentation or compilation failure rejects the collection; changing the
pin requires running those checks and a complete native collection again.

The metric follows the tool's limitations: `range` loops, unconditional loops
and `select` arms are not instrumented. Code without decisions contributes no
branch outcomes. Platform files excluded by the native Go build are outside
that run's denominator. These are instrumentation limits, not a package
allowlist; business packages are never removed to raise the percentage.

The semantic basis is the upstream
[`-branch` release](https://github.com/rillig/gobco/releases/tag/v1.3.0),
[v1.3.3 instrumenter](https://github.com/rillig/gobco/blob/v1.3.3/instrumenter.go)
and [counter implementation](https://github.com/rillig/gobco/blob/v1.3.3/templates/gobco_fixed.go).
The [Go coverage documentation](https://go.dev/doc/build-cover) describes the
separate statement-based profiles. The existing Cobertura XML is a statement
report and is not used to compute the branch gate.

## Package and test scope

Both metrics start from `TEST_PACKAGES`:

```text
./internal/... ./pkg/... ./cmd/... ./specs/...
```

The only package exclusion is `cmd/specgen`, matching the existing statement
gate's generator exclusion. Other command packages and generated production
Go files remain in scope. Packages without tests remain in the denominator;
their decisions have zero hits. Packages without decisions are recorded with
zero branch outcomes. Test files, standard-library code and external module
dependencies are not measured as production code.

Gobco runs each package's own tests, including black-box tests, against a
temporary instrumented copy. Tests of a different package do not contribute
hits to that package's branch report. This differs from the existing statement
gate's `-coverpkg` collection across all selected package tests. Keep this
difference when interpreting the two percentages.

The separate `e2e/go.mod` module is **not included** in either root unit-test
metric. Its unit tests, registry/lint checks and live tests retain their existing
gates. Website and DSH plugin JavaScript/TypeScript coverage is also outside this
Go metric. Collecting branch coverage does not run live cloud tests.

The canonical CI measurement runs natively on Linux. The report records the
actual Go version and host OS/architecture. Local reports on another platform
are useful for development but cannot substitute for the Linux baseline. The
collector uses the native host, `GOWORK=off` and default build tags, so a
cross-compilation target or custom `GOFLAGS` cannot silently change its scope.

The collector keeps its source/tool workspace under the report directory and
allocates a separate private test root through the OS temporary facility. The
temporary base is canonicalized and must have no non-directory `go.mod` in its
ancestors. If the selected temporary base is inside a module, collection fails
explicitly rather than changing module-discovery tests. All three variables are
set only in command environments. Each run cleans only its freshly allocated roots,
preserving their parents and unrelated siblings, and records the paths and
cleanup result in `coverage.json`. Cleanup failure rejects the collection.

## Reports and baseline collection

```sh
# Collect a baseline even when it is below 85%; collection/test errors still fail.
make branch-coverage-report

# Exercise the parser, gate and actual pinned tool against an isolated fixture.
node --test .github/scripts/branch-coverage.test.mjs

# Isolate the generated runtime's subprocess/resource-limit contract.
node --test --test-name-pattern='runtime templates preserve' .github/scripts/branch-coverage.test.mjs

# Check independent temporary-root semantics with the unchanged DefaultDir test.
node --test --test-name-pattern='temporary workspaces' .github/scripts/branch-coverage.test.mjs
```

Reports are written under `reports/branch-coverage/`:

- `coverage.json`: source commit/status, tool provenance, platform, expected
  packages, package totals, aggregate counts and threshold result.
- `summary.md`: totals and uncovered outcomes with source positions.
- `packages/**/*.json`: unmodified gobco counters for each package.
- `packages/**/*.log`, `tool.log`, `tool-build.log` and `tool-version.log`:
  commands, exit codes, termination signals and diagnostic output.

Each run replaces its previous branch report. Keep baseline and candidate
artifacts separately. CI uploads `ut-branch-coverage-main` even when collection
or the threshold fails. Missing profiles, invalid counters, incomplete package
inventories, failed tests and empty aggregate data fail both collection modes.
`--report-only` bypasses only the percentage comparison; `threshold_met` remains
false if the measured ratio is below 85%.

To measure a historical baseline with the identical collector, check out that
commit in an isolated worktree, prepare its metadata, and run the candidate
collector by absolute path from the baseline worktree root:

```sh
node /absolute/path/to/candidate/.github/scripts/branch-coverage.mjs --report-only
```

Archive the report and logs with the actual commit and platform. Then run the
normal gate on the candidate and use its uncovered-outcome list to add focused
behavioral tests. A baseline is a measured artifact, not a percentage inferred
from the existing statement report. No baseline percentage is recorded here
until a complete canonical native run has produced one.

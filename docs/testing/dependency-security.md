# Dependency security

GitHub matches the default branch dependency graph against the GitHub Advisory
Database whenever the graph or advisories change. An alert identifies an
affected package and manifest, a vulnerable version range, and the first
patched version when available. Check the actual dependency path and how the
package is used to assess exposure in ecctl. [Dependabot alerts](https://docs.github.com/en/code-security/concepts/supply-chain-security/dependabot-alerts)

When security updates are enabled in repository settings, Dependabot attempts
to open a PR for the minimum compatible fix. Some transitive updates require
changing the parent package or its version constraint. Scheduled version
updates are configured in `.github/dependabot.yml` and have a separate purpose.
[Security updates](https://docs.github.com/en/code-security/concepts/supply-chain-security/dependabot-security-updates),
[Version updates](https://docs.github.com/en/code-security/concepts/supply-chain-security/dependabot-version-updates)

## Read the open queue

From the repository root, run `make review-dependabot` with Node.js and an
authenticated `gh` CLI that supports `gh api --slurp`. The command reads all
pages of open alerts on GitHub and prints JSON with severity, manifest, scope,
relationship, advisory IDs, and first patched version. It defaults to
`aliyun/elastic-compute-control-cli` and honors `GITHUB_REPOSITORY` for another
GitHub.com repository in `owner/repository` form. Use a repository account with
access to the alerts and a credential with Dependabot alerts read permission.
The same queue is visible under Security and quality → Dependabot.
[Repository alert API](https://docs.github.com/en/rest/dependabot/alerts#list-dependabot-alerts-for-a-repository)

A successful report contains an `open` count, including `0` for an empty queue.
An API, permission, or response error exits unsuccessfully. Record the queue as
unverified if the command fails. Read the queue on new notifications and review
the remaining open alerts weekly.

## Triage and close alerts

1. Record the alert link, GHSA, manifest, installed version, and the dependency
   path. Check all installed copies and group alerts that share a fix. Use
   `go mod why -m <module>` in the relevant Go module or run
   `npm explain <package>` in the npm workspace after `npm ci`.
2. Triage Critical and suspected malware alerts the same day. Assess exposure
   in the shipped CLI, hosted site, local development, and CI or release jobs.
   Assign High alerts promptly and reassess Medium and Low alerts in the weekly
   review. Check advisory prerequisites, untrusted inputs, and credentials or
   publish permissions available during execution. A dependency used during a
   build can matter even when the published artifact contains only static files.
3. Prefer a compatible patched parent or dependency version. Review the
   Dependabot PR's version and lockfile changes against current `main`, or make
   a focused update in the affected workspace. Check every affected range
   before using a scoped npm override. A cross-major override needs validation
   of the parent's use of the dependency and the publish job's Node.js version.
4. Run the checks for the affected surface. Use `govulncheck` to supplement Go
   triage with call-path evidence and `npm audit` to check the resolved npm tree.
   Review the diff and the CI dependency review before merging.
5. After the fix reaches `main`, check the graph and confirm GitHub marks the
   alerts fixed. For an accepted exception, record the evidence, owner,
   mitigation, and review date before dismissing an alert. Revisit the decision
   when the dependency, its usage, or the advisory changes.
   [Managing alerts](https://docs.github.com/en/code-security/how-tos/manage-security-alerts/manage-dependabot-alerts/view-dependabot-alerts)

| Changed surface | Validation |
| --- | --- |
| Root `go.mod` or `go.sum` | `make metadata-prepare`, `make lint`, `make test`, and the affected package or release checks. For telemetry upgrades, cover `pkg/telemetry` and the release build. |
| `e2e/go.mod` or `e2e/go.sum` | `make metadata-prepare`, `make -C e2e test`, and `make -C e2e lint`. |
| `website/` npm dependencies | `make review-website` for install, tests, typecheck, and production build using the Pages workflow's Node.js version. |
| `dsh-plugin/` npm dependencies | `make review-dsh-plugin` for install, tests, and package contents. |
| `.github/workflows/` action references | Review upstream release notes and verify the jobs that use the updated action. |

## Review gates

The required `CI / test` job uses GitHub Dependency Review on PRs. It compares
the base and head dependency graphs and fails for newly introduced High or
Critical findings in runtime, development, or unknown scope. The step uses the
workflow's `contents: read` permission. Graph coverage depends on the ecosystem
and manifest. Review the effective package versions and build inputs when the
graph cannot see them. [Dependency Review](https://docs.github.com/en/code-security/concepts/supply-chain-security/dependency-review),
[Supported ecosystems](https://docs.github.com/en/code-security/reference/supply-chain-security/dependency-graph-supported-package-ecosystems)

`make review-final` and CI exercise the report offline with a fake `gh` through
`make test-dependabot-report`. Run the authenticated `make review-dependabot`
explicitly for the default-branch backlog. The PR check and the queue answer
different questions and each is needed when triaging a security update.

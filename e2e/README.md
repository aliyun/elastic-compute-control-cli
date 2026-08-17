# ecctl-e2e

End-to-end test suite for ecctl, run as a standalone CLI against **real**
Alibaba Cloud. The live CI workflow is manually dispatched. This independent
Go module drives the `ecctl` binary as a subprocess.

## Layout

```
cmd/ecctl-e2e/      CLI (run / sweep / coverage / lint / report)
internal/           runner, exec, scenario, match, report, sweeper, coverage, ...
cases/              human-maintained lifecycle cases (AI-drafted)
CASE_AUTHORING.md    reusable rules and definition of done for new cases
OPERATION_COVERAGE.md operation-gap reasons, fixtures and cleanup contracts
e2e.yaml            ordered region profiles + repository paths
fixtures/           shared stack and dynamic-parameter policy
sweep.yaml          sweepable kinds for the cleanup safety net
```

## Quick start

```bash
make build         # build the runner and the public ecctl binary
make lint          # validate case/stack/run-config/dynamic-parameter contracts
make coverage-resources # reject implemented resources that have no case
make coverage-operations # reject declared operations that have no case step
make coverage      # public-surface live-pass completion gate
make list          # list discovered public cases (run --collect-only)
make validate-full # collect and validate public + hidden cases offline
```

The runner accepts the binary explicitly. `make build-public` produces
`bin/ecctl-public`; `make build-full` produces `bin/ecctl-full`. Keep both
artifacts: the first run targets only the public surface, while a later run
uses the full surface for resources intentionally hidden by the default CLI.

```bash
ecctl-e2e run --surface public --ecctl-bin bin/ecctl-public --report-dir reports
ecctl-e2e run --surface full --ecctl-bin bin/ecctl-full --report-dir reports
```

Before a non-dry run the runner calls `ecctl capabilities --output json`,
checks the surface marker, and rejects any case operation absent from that
binary. This prevents accidentally running hidden commands with the public
binary (or vice versa).

## Coverage registry

`coverage.yaml` is the machine-checkable status registry for case-backed
operations declared under `../specs`. Operations without a case are omitted;
resources without any case-backed operations are omitted as well.

Registry version 3 groups entries by canonical product and resource names from
the specs. Resource aliases are accepted by the CLI but are never written as
registry keys. For example, `cluster` is an alias of the canonical ACK resource
`ack`, so its entry is:

```yaml
version: 3
resources:
  ack:
    ack:
      operations:
        create:
          status: live-pass
          case: cases/ack/ack-lifecycle.yaml
          fingerprint: sha256:...
          time: "2026-07-14T16:30:43+08:00"
          reason: live-verified
```

E2E case commands should likewise use canonical resource names; aliases are a
CLI convenience, not coverage identities.

Initialize or refresh it after changing specs or cases:

```bash
ecctl-e2e coverage registry init --specs ../specs --cases cases --registry coverage.yaml \
  --ecctl-bin bin/ecctl-public
```

When init reads a version 2 registry, it migrates the flat `product/resource`
keys to version 3 and preserves all five fields of every unchanged operation.

Init requires either `--ecctl-bin` or `--capabilities`. The supplied capability
document must identify the `public` surface and is used to generate the
top-level completion summary:

```yaml
summary:
  surface: public
  resources: 27
  operations: 142
  missing_cases: 0
  passed: 133
  not_passed: 9
```

`resources` and `operations` count the capabilities exposed by the public
binary. `missing_cases` counts public operations with no registry entry;
`passed` counts public `live-pass` operations; and `not_passed` counts public
`offline` operations. Therefore `operations` always equals `missing_cases +
passed + not_passed`. A check supplied with public capabilities recomputes these
counts and rejects a stale summary.

Validate it without cloud credentials. Pass the selected binary when the
registry is used as a surface-specific gate:

```bash
ecctl-e2e coverage registry check --specs ../specs --cases cases --registry coverage.yaml \
  --ecctl-bin bin/ecctl-public --surface public --fail-on-not-live
```

Print status counts for CI summaries:

```bash
ecctl-e2e coverage registry summary --registry coverage.yaml --output json
```

The fast spec-to-case audit is separate from the live registry:

```bash
ecctl-e2e coverage --specs ../specs --cases cases --fail-on-gap
```

Its JSON includes operation totals (`declared`, `covered`, `gaps`) and resource
totals (`declared_resources`, `covered_resources`, `resource_gaps`). A resource
is covered only when at least one declared operation appears in a case whose
top-level `resource` matches it; fixture/helper steps in another resource's case
do not satisfy the gate. `--fail-on-resource-gap` is the coarse resource audit.
`--fail-on-gap` is the completion gate and requires every declared operation to
appear as a real first-class `ecctl` step.

The registry has two statuses:

- `offline`: the operation has a case but has not been accepted by a real cloud
  run against the current case contents.
- `live-pass`: the current case contents passed a real cloud run.

Every operation has the same five required fields:

- `status`: `offline` or `live-pass`.
- `case`: path to the case that covers the operation.
- `fingerprint`: `sha256:` plus the SHA-256 digest of the complete raw case YAML
  bytes.
- `time`: RFC3339 time of the validation or review represented by the status.
- `reason`: `live-verified`, `not-run`, `case-changed`, `prerequisite`,
  `test-failed`, or `unknown`. `live-pass` requires `live-verified`; `offline`
  accepts the other five values.

`registry init` preserves all five values of an unchanged entry, so repeated
initialization produces byte-identical canonical YAML. If its case path or raw
contents change, init writes the current fingerprint and time and resets the
entry to `offline` with reason `case-changed`. A new case-backed operation starts
as `offline/not-run`. Detailed prerequisite and test failures remain in the run
report; the registry stores only the reason enum and is not updated implicitly
from the latest report. A normal check permits declared operations with no case;
`--fail-on-not-live` treats both omitted and `offline` selected operations as a
failed completion gate.

## Run configuration, dynamic parameters, and execution order

`e2e.yaml` is the top-level run configuration. Each ordered
`regions.candidates` entry is an atomic region profile: it binds one region ID
to the account resources usable in that region. The checked-in file contains
only region IDs and repository paths. Copy it to the ignored `e2e.local.yaml`,
add the account resources required by the selected cases, and run with
`--config e2e.local.yaml`. E2E prerequisite values are intentionally not read
from environment variables; cloud credentials still use ecctl's normal
credential chain. Test resource names always use the fixed `ecctl-e2e` prefix.

The supported prerequisite bundles and fields are:

```yaml
version: 2
regions:
  candidates:
    - id: cn-example
      prerequisites:
        ack.auto_repair_policy: {}
        lingjun.cluster:
          node_group_ids:
            - ng-e2e-a
            - ng-e2e-b
paths:
  cases: cases
  stack: fixtures/stack.yaml
  coverage: coverage.yaml
  parameter_policy: fixtures/parameter-policy.yaml
```

The example values are placeholders and must be replaced with real,
region-matching resources before a live run. Do not commit account-specific
`e2e.local.yaml` files. These are the only supported prerequisite fields; the
loader intentionally does not enforce a field allowlist yet. Missing values
for the account prerequisites below cause dependent cases to be skipped with a
warning before any mutating case step runs.

`ack.auto_repair_policy: {}` is a full-surface-only capability marker; public
ACK cases do not read it. Declare it only after Alibaba Cloud support has
allowlisted the account for ACK auto-repair policies. The disposable ACK
cluster, node pool, diagnosis worker and trigger parent are created by
`fixtures/stack.yaml`.

Public ACK cases require no manually declared ACK prerequisites. The addon
lifecycle selects compatible names and versions from the live ACK catalog;
KubeConfig expiration update is not part of the supported command surface, and
operation plans are hidden from the public surface.

ECS cases require no account resource values in `e2e.local.yaml`. The image
lifecycle creates a run-unique private OSS bucket, exports a RAW image, downloads
and safely extracts the archive in the runner's private temporary directory,
uploads the uncompressed RAW object for import, and removes the objects and
bucket. The prepaid instance is intentionally renewed for one month. The two
`lingjun.cluster` node group IDs are used only by
the node inventory and cluster scaling cases. They must be different, and each
group must expose at least one compatible free node in this region.
The basic Lingjun cluster and node-group lifecycles do not require free physical
nodes and need no manually declared `lingjun.cluster_network` values: preflight
auto-discovers the zone, derives its HPN zone, selects a compatible public
machine type and image, and picks a usable resource group. The shared stack
then provisions the VPC, a dedicated vSwitch and security group, and a CUSTOM
Lingjun ENI as the cluster-network anchor. The fixture creates the key pair and,
for the node-group case, the parent Lite cluster with a non-empty bootstrap
node-group descriptor but without assigning `Nodes`; the case creates only the
target node group. Both lifecycles use a 180 GiB PL1 ESSD system disk, avoiding
the local-disk allowlist required by some Lingjun machine types. Neither
lifecycle consumes the configured `lingjun.cluster.node_group_ids`. A run that
cannot create these resources (for example, no public Lingjun zone or no
compatible machine/image in the region) may still pin an explicit
`lingjun.cluster_network` bundle in the region profile, naming an existing
CUSTOM ENI and its resource group, VPC/VPD, vSwitch/subnet, security group, HPN
zone, zone, machine type and image; the read-only preflight probe then verifies
the ENI and its network ownership before any mutation.

Most declarative bundles are safety/capability inputs rather than pre-existing
resources. A bundle may identify a protected, externally onboarded resource
only when the suite cannot create and safely remove it. Before planning
mutations, preflight removes a bundle whose documented fields are missing or
empty, so the dependent case is skipped instead of running with a partial
cleanup contract.

Before building live execution units, the runner checks the configured Lingjun CUSTOM ENI for cluster-network
ownership, and queries Lingjun free-node inventory for both scaling node groups.
An empty value, an ENI metadata mismatch, a missing free
node, or an incompatible pair removes that prerequisite from the region
profile; if no selected profile remains usable, dependent cases are reported as
skipped and a `warning:` line is emitted. Permission, authentication, network,
malformed response, and unknown API errors remain fatal so infrastructure
failures are not hidden as missing resources. `--collect-only` and `--dry-run`
do not call these live probes.

Cases declare suite-level `requires_prerequisites` for hard requirements shared
by every operation. DAG steps may declare optional requirements; profiles
satisfying more optional requirements are tried first, but a missing optional
bundle skips only that step and its descendants. Cases may declare additional
named roles under `region_requirements`. Cross-region copy cases use
a `destination` role with `distinct_from: primary`. After capability validation
and the supported read-only prerequisite probes, the planner groups runnable
cases by requirement signature and enumerates only complete assignments. An
unknown missing bundle fails preflight; region fallback happens only between
complete assignments and only for a region-unavailable failure. Independent
execution units continue and are reported separately.

ACK clusters, test nodes and RAM roles, along with resource groups and policies
that can be created safely, are created by the shared stack or their cases
rather than supplied as account-level values. Lingjun VPD CIDRs are case data,
not prerequisite configuration.
Compatible ACK operations reuse the run-lifetime `ack_shared_cluster`. The
`ack/ack` lifecycle owns the second cluster and covers the destructive
`ack/nodepool` and `ack/task` operations on it. It completes control-plane and
node-pool upgrades, exercises pause/resume/cancel on one node-pool upgrade task,
releases a worker, and deletes the cluster as the final reset boundary. A full
ACK run therefore provisions two clusters instead of six.
`fixtures/stack.yaml` is the shared resource dependency graph and does not
select a region. All creatable prerequisite resources live in this fixture;
case steps do not create them. Suite and DAG-step `needs` entries are stack node IDs: the
runner selects those nodes plus their transitive dependencies and does not
provision unrelated branches. Each fixture node declares its canonical
`resource`, whether it is a `create` or `lookup` node, and its lifetime.
`lifetime: execution` is the default. Compatible `lifetime: run` nodes are
leased once per top-level run, keyed by surface, binary, primary region, and
fixture ID; a rendered-command mismatch is a setup error. Use
`run --collect-only -v` for a readable expansion or
`run --collect-only --output json` for `direct_needs` and the complete fixture
closure. Each stack node declares its own `requires_params`, so selecting only
`vpc` or `security_group` does not trigger zone, instance, disk, or image
discovery. `fixtures/parameter-policy.yaml` supplies the ordered ECS
`cores` candidates; instance types, system disks, and data disks are discovered
from `DescribeAvailableResource` and the image is selected with `DescribeImages`.
Step-level `depends_on`, `needs`, and `requires_prerequisites` are valid only
with `execution: dag`; sequential cases may still declare step `locks`, while
case-wide fixture and prerequisite requirements stay at the suite top level.
In `auto` mode the runner performs only the read-only inventory queries required
by the selected parameter keys. A zone-only request uses the ECS zone inventory.
Cases may add `parameter_constraints.ecs.min_eni_quantity` or
`allowed_system_disk_categories`; the planner isolates different capability
signatures, and the resolver validates instance metadata and disk inventory
before provisioning that execution unit. An unavailable constrained tuple skips
only that unit. ACK metadata first lists creatable Kubernetes versions and then
queries upgrade targets for each candidate. Upgrade-only cases are isolated and
skipped when no path exists, while ACK CRUD cases continue. The ACK containerd
version and test-node profile still come from the same bounded inventory pass.
ECS cases consume the resulting region/zone, image and disk-category values.
Lingjun scaling discovery checks both configured node groups and derives their
shared HPN/zone/machine profile. Basic cluster and node-group cases instead use
the auto-discovered or explicitly pinned `lingjun.cluster_network` values.
`IoOptimized` is sent as the ECS API value `optimized` (the API does not accept
the literal boolean `true`).

The first complete region assignment is attempted first. A later assignment is
used only when every failure in the attempt is classified as region/zone
unavailable. Credentials, permissions, assertions, quota errors, and cleanup
failures do not trigger fallback. Each attempt is cleaned up before the next
assignment starts. Cleanup journals, manifests and report entries record the
execution ID, role and concrete region mapping.

The run order is deterministic:

1. load and validate all selected cases, stack, run config, and parameter
   policy;
2. build complete region-role assignments, then resolve the selected primary
   region's dynamic ECS parameters;
3. topologically provision only the selected shared-stack closure (`vpc` →
   `vswitch`/`security_group` → dependent resources), registering teardown
   immediately after each create; if one branch fails, independent branches and
   their cases continue, while only cases whose closure contains a failed node
   are skipped;
4. run commands under one global operation semaphore. Legacy cases keep their
   ordered fail-fast lifecycle; `execution: dag` cases run ready operations
   concurrently, skip only descendants of failed or missing dependencies, and
   use rendered keyed locks for shared mutations;
5. tear down each case in reverse registration order, execution-lifetime
   fixtures after their unit, and run-lifetime fixtures once in reverse
   dependency order after all units;
6. run the tag-based sweeper after the run to remove resources left by either
   a failed teardown or an interrupted process.

The cleanup journal is written after every registration. Its filename includes
the execution, attempt and non-primary role when needed. Metadata records run
ID, execution, role, region, surface and binary, so `sweep replay` refuses a
mismatched recovery command.

## Static case lint

`lint cases` is the offline gate for case authoring. It loads every case via the
same `scenario` parser as `run --collect-only`, then checks cross-file rules
that collection alone cannot see:

- every `{{.inputs.*}}` template reference has a fixture key;
- every suite/step `needs` entry names a real stack node, and every `.stack.*`
  reference is produced by the step's suite/ancestor dependency closure;
- stack node IDs and capture providers are unique, and each node declares the
  dynamic parameter keys used by its own templates;
- DAG dependencies are acyclic, captures are used only by descendants, and lock
  templates use variables available to that operation;
- current-step captures may be used by that step's teardown;
- taggable create commands include `--tag ecctl-e2e=1` and
  `--tag run-id={{.run_id}}`; untaggable RAM roles and policies are protected
  by their cleanup journal instead;
- create commands include teardown;
- `coverage.yaml` case references point to loaded case files.

Run it without cloud credentials:

```bash
ecctl-e2e lint cases --config e2e.yaml
```

A real run needs `ecctl` on PATH and Alibaba Cloud credentials in the
credential chain (STS via OIDC in CI). The default `e2e.yaml` can run cases that
need no account prerequisites. Protected-resource cases use the local region
profile config:

```bash
ecctl-e2e run --config e2e.local.yaml --surface public \
  --ecctl-bin bin/ecctl-public --report-dir reports
```

The ECS renewal branch creates its own one-month prepaid instance, renews it,
and verifies the renewed expiration. DeleteInstance rejects in-warranty prepaid
instances, so the read-back step registers a `ModifyInstanceChargeType` teardown
that unlocks deletion (even when the async conversion does not take effect);
the create step's `delete --force` teardown then releases the instance, because
teardowns run in LIFO order. No account-owned instance ID is required:

```bash
ecctl-e2e run --config e2e.local.yaml \
  cases/ecs/instance-lifecycle.yaml
```

`--report-dir` writes `e2e-report.{json,html,xml}`; under GitHub Actions a GFM
summary is appended to `$GITHUB_STEP_SUMMARY` automatically. JSON reports use
`schema_version: 2`; multi-execution region mappings, attempts, and resolved
parameters live under `executions`, while the legacy top-level fields remain
populated for single-execution runs.

Validate a live report with a deterministic gate:

```bash
ecctl-e2e report check reports/e2e-report.json --failed 0
```

## Selecting cases

Selection follows pytest. Positional **targets** are node ids and `-k` is a
boolean keyword expression; with no selector, every case under `--cases` runs.

```bash
ecctl-e2e run cases/vpc/                       # a directory
ecctl-e2e run cases/ecs/eni-lifecycle.yaml     # one case file
ecctl-e2e run cases/vpc/vpc-lifecycle.yaml::update   # legacy prefix, or DAG step plus ancestors
ecctl-e2e run -k "vpc or eni"                  # keyword expression
ecctl-e2e run -k "ecs and not snapshot"
ecctl-e2e run --collect-only                   # list selected cases (also validates)
ecctl-e2e run --collect-only -q                # one path per line; -v lists step ids
```

`--config e2e.yaml` (default, applied if present) loads the structured run
configuration. Explicit CLI flags still win; `--region` pins the primary role,
while named roles still follow their declared constraints. Exit codes follow
pytest: `0` ok, `1` cases failed, `2` interrupted, `4` usage error, `5` no
cases selected (`--exit-zero` forces `0`).

## Authoring cases

A legacy case is an ordered list of steps. Each step declares exactly one full
`ecctl` command in `run`, except the constrained `local` action used by the ECS
image lifecycle to extract one `.raw` regular file from a `.tar.gz` archive.
Local paths must be direct children of the runner's private `{{.work_dir}}`;
the runner invokes no shell and rejects links, traversal, non-RAW files and
multi-file archives. A case with
`execution: dag` is an operation graph: `depends_on` defines ordering, step
`needs` and `requires_prerequisites` localize resource gates, and `locks`
serialize mutations of the same rendered resource key. Both forms use
declarative matchers. AI may draft a case from
a spec; a human then fills the independent assertions (read-back / differential)
that the spec cannot know — that is what gives the suite its correctness power.
Every declared `capture` is required: if its path is absent, that same step
fails. Legacy cases stop; DAG cases skip only descendants, and captures are
published only to those descendants.
Use `{{.resource_prefix}}` for cloud resource names: it is capped at 40
characters and carries a stable hash when truncated. Keep `{{.run_id}}` in tags
and reports so cleanup and evidence retain the complete run identity.

Follow [CASE_AUTHORING.md](CASE_AUTHORING.md) for the reusable decision rules,
required cleanup model, registry workflow and definition of done.

## Cleanup

Three layers: per-case + stack teardown stacks (signal-safe), a run-specific
cleanup journal and manifest, and `ecctl-e2e sweep` as the safety net. Runtime
finalizers may use safe lifecycle reversals such as ACK `revoke` or `detach`,
but the persisted crash-recovery journal contains validated `ecctl ... delete`
commands plus exact OSS `DeleteObject` and `DeleteBucket` calls only. Arbitrary
raw API calls remain ineligible for unattended replay.
Cleanup failures include timeout state, cloud/action error codes, and redacted
stdout/stderr in the report; credential-like values are scrubbed before logging.

If a process dies before teardown completes, replay the exact journal (the
recorded binary, execution, role and region are used unless explicitly
overridden). Multi-execution and fallback runs add
execution/attempt/non-primary-role suffixes:

```bash
# Single-region run:
ecctl-e2e sweep replay reports/cleanup-journal-<run-id>.json
# Multi-region execution (choose the role journal that created the resources):
ecctl-e2e sweep replay reports/cleanup-journal-<run-id>-<execution>-attempt-1-destination.json
```

`sweep` lists each tagged kind and deletes orphans, selected by `--ttl`
(default 24h; use `--ttl 0` to delete everything tagged regardless of age) or
`--mode finished-run`. Kinds run in config order so dependencies hold
(instances before their vswitches/vpcs); deletes within a kind run in parallel,
bounded by `--concurrency` (default 20). Failures are reported per resource with
the underlying ecctl error.

Validate cleanup coverage without calling the cloud:

```bash
ecctl-e2e sweep check --cases cases --config sweep.yaml
```

The check validates `sweep.yaml` has complete list/delete metadata and every
create step has teardown plus a matching sweep kind or a reviewed
`non_sweepable` reason.

## CI scheduling

PR CI runs the repository lint/tests, the independent E2E module unit tests,
case/config lint, coverage-registry consistency check, sweep-coverage check,
and release snapshot build. It does not require Alibaba Cloud credentials and
does not require every registry operation to have live-pass evidence.

Live CI uses GitHub OIDC to exchange a job identity for temporary Alibaba Cloud
STS credentials. Keep the following controls in place together:

- The RAM role trust policy must match the actual GitHub `oidc:sub` values for
  this repository's `live-e2e` and `live-e2e-sweeper` Environments with
  `StringEquals`; issuer and audience checks alone trust jobs from other GitHub
  repositories.
- Both GitHub Environments must restrict deployments to the default branch and
  require a reviewer. The sweeper's `workflow_run` trigger remains automatic,
  but its deployment must wait for approval because the cloud APIs do not
  provide a uniform tag condition for every delete action in its role.
- The RAM role must carry only the API actions and resources required by the
  selected E2E surface. The automatic sweeper uses the separate
  `ecctl-e2e-sweeper-ci` delete-focused role instead of weakening the nightly
  role or Environment protection rules.
- Actions in jobs that obtain cloud credentials stay pinned to full commit
  SHAs, and STS sessions stay bounded by the corresponding job timeout.

Manually dispatched live CI materializes the protected `live-e2e` environment secret
`ECCTL_E2E_CONFIG_YAML` as ignored `e2e.local.yaml`, validates that file, and
runs the selected suite using its ordered region profiles. The secret contains
the complete version 2 YAML document shown above; the runner itself never
resolves prerequisite values from environment variables. Manual dispatch can
pass a single-region `--region` override and a `-k` expression:

```bash
ecctl-e2e run --config e2e.local.yaml -k "vpc or eni" --report-dir reports --exit-zero
```

Reports are written under `reports/`, which is ignored by Git. The current
workflow keeps reports local (and as a CI artifact); no OSS Bucket or report
upload credential is required. The coverage registry does not read those local
report files; validate a report explicitly with
`ecctl-e2e report check <path> --failed 0`.

The sweeper workflow runs automatically after each same-repository
`e2e-nightly` completion and can also be dispatched manually. Automatic runs
use cleanup mode: first a dry-run audit for every candidate region declared in
checked-in `e2e.yaml`, then `sweep --mode finished-run`. Manual dispatch inputs
can select audit-only mode or override one region. Run
`ecctl-e2e sweep check --cases cases --config sweep.yaml` locally before
changing sweep coverage.

Validate workflow syntax locally with:

```bash
actionlint ../.github/workflows/*.yml
```

If `actionlint` is not installed, use an equivalent local YAML/action syntax
parser and record the replacement command in verification evidence.

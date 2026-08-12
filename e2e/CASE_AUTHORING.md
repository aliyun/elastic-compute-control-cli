# E2E case authoring rules

These rules apply whenever a resource spec is added or an implemented resource
has no case. The objective is a case that can be run against Alibaba Cloud,
fails on a real contract regression, and does not endanger unrelated resources.

## 1. Audit the real command surface

Build both binaries and use their capability documents as the source of truth:

```bash
make build-public build-full
bin/ecctl-public capabilities --output json
bin/ecctl-full capabilities --output json
bin/ecctl-e2e coverage --specs ../specs --cases cases --output json
```

Do not infer public or hidden status from a spec filename. Distinguish these
states explicitly:

- resource gap: no case names the resource as its primary `resource` and
  exercises one of its declared operations;
- operation gap: the resource has a case, but some operations are not covered;
- offline: a case exists, but its current bytes have not passed a live run;
- live-pass: the current case fingerprint passed a live run.

`make coverage-resources` rejects resource gaps. `make coverage-operations`
rejects every operation gap. A resource is not complete merely because one
list/get command names it.

## 2. Use canonical, resource-first identities

- Put the file under `cases/<product>/`.
- Name it `<resource>-lifecycle.yaml`.
- Set `resource: <product>/<canonical-resource>` from the spec. Do not use a
  CLI alias as the registry identity.
- Keep exactly one primary case for each canonical resource. All declared
  operations, including read, mutation, upgrade and protected actions, belong
  to that lifecycle case. Lint rejects a second case with the same primary
  `resource`.
- Set `surface: full` only when the resource or operation is absent from
  `ecctl-public capabilities`.
- Keep one primary resource per file. If a tightly coupled child API can only
  be verified inside the parent's lifecycle, declare it explicitly with
  `covers: [product/child-resource]` and execute its real command in the case.
- Use first-class `ecctl <product> ...` commands for covered operations.
  `ecctl call` is allowed for auxiliary setup, verification and cleanup, but it
  never counts as coverage.

## 3. Choose the safest valid case shape

Use the first applicable shape:

1. Lifecycle: create, capture the ID, independently get/list it, update it,
   delete it, and assert deletion. This is the default for disposable resources.
2. A resource that exposes only list/get still uses its ordinary
   `<resource>-lifecycle.yaml`; do not encode "readonly" in the case identity.
3. Protected operations declare a dotted step-level
   `requires_prerequisites` bundle only for an account capability, inventory,
   paid irreversible target, or external object that ecctl cannot create and
   safely remove. Keep the suite-level field only when every operation has the
   same hard requirement.

Never select an arbitrary item from a list and mutate or delete it. A protected
prerequisite must be documented as dedicated and disposable in
`e2e.local.yaml`. In a DAG case, a missing step prerequisite skips only that
operation and its descendants; independent operations continue.

For a mutation that needs dedicated inventory, write the real operation step
in the same resource lifecycle and gate that case with a validated
fixture/prerequisite; the operation remains `offline` until that exact case
passes live. Do not add placeholder commands or weak raw API calls merely to
improve counts.

## 4. Make inputs isolated and reproducible

- Use `{{.resource_prefix}}` for names and `{{.run_id}}` for evidence.
- Add both `--tag ecctl-e2e=1` and `--tag run-id={{.run_id}}` to every taggable
  create.
- Every creatable prerequisite resource must be a node in
  `fixtures/stack.yaml`; cases declare only its fixture ID through `needs`.
  Do not create prerequisite resources in case steps.
- Every fixture node declares `resource: product/resource`, `mode: create` or
  `lookup`, and a lifetime. The default `lifetime: execution` is cleaned after
  one execution unit. Use `lifetime: run` only for compatible resources that
  should be leased across units; a run-lifetime node may depend only on other
  run-lifetime nodes.
- Reuse one run-lifetime parent for compatible operations, but isolate
  destructive mutations that change shared state. ACK uses two clusters: the
  run-lifetime `ack_shared_cluster` for reversible cases, and the cluster owned
  by `ack/ack` for control-plane upgrades, node-pool upgrades, task control and
  worker release. The latter is deleted only after that composite lifecycle.
- Use `requires_params` for discoverable regional inventory.
- Use `requires_prerequisites` only for account resources that cannot be
  discovered or safely created by the suite.
- A protected externally onboarded resource, such as a Lingjun CUSTOM ENI and
  its network ownership tuple, may be a prerequisite only when preflight
  validates the exact identity and the case never mutates or deletes it.
- Keep account IDs, credentials, bucket names and dedicated resource IDs in
  ignored `e2e.local.yaml`, never in a tracked case.
- If only some operations need a costly or protected prerequisite, keep them in
  the single resource lifecycle case, set `execution: dag`, and declare the
  requirement on those steps. Do not split the resource by cost, mutability or
  protection level.

In `execution: dag` cases:

- `depends_on` names predecessor steps. A failed or skipped predecessor blocks
  only its descendants.
- `needs` requests fixtures for that step. Suite-level `needs` remains a hard
  default shared by every step.
- `locks` contains rendered resource keys such as
  `ack-cluster:{{.stack.ack_shared_cluster}}`. Operations with the same key are
  serialized; unrelated keys may run concurrently.
- Captures are visible only to descendant steps. Independent branches must not
  consume each other's captures.

Sequential cases may still use step `locks`, but operation-level `depends_on`,
`needs`, and `requires_prerequisites` are rejected there: switch the case to
`execution: dag` or move a true case-wide requirement to the suite top level.

Inspect direct and transitive fixture dependencies before running:

```bash
bin/ecctl-e2e run --collect-only -v
bin/ecctl-e2e run --collect-only --output json
```

The JSON output includes suite and step `direct_needs`, `depends_on`,
`requires_prerequisites`, `locks`, plus the topologically ordered `fixtures`
closure with each fixture's resource, mode, and lifetime.

## 5. Assert behavior, not only exit code

- Every case must contain an independent output-shape or value assertion.
- A create must capture the provider ID and register teardown on that same step.
- Read back created or updated state with get/list when the API supports it.
- Prefer stable fields: IDs, names, requested values, status classes and array
  shape. Do not assert request IDs, timestamps, ordering, or provider-generated
  text unless they are the contract under test.
- A tautological list assertion is acceptable only when the resource exposes
  no mutations; mutation lifecycles must also assert resource identity or a
  state transition.

## 6. Design cleanup before mutation

Cleanup has three layers:

1. immediate step teardown, registered with the create;
2. persisted cleanup journal replay;
3. tag-based sweeper for supported resource kinds.

Use an explicit final delete step as the normal-path assertion even when the
create step already has teardown. Add a new sweep kind for taggable resources.
If the provider cannot filter by tags, list the resource under
`sweep.yaml: non_sweepable` with a reason and review date. Do not create a
resource that has neither a delete API nor an explicit retention decision.

## 7. Refresh the registry without overstating evidence

After case changes, run from `e2e/`:

```bash
bin/ecctl-e2e coverage registry init \
  --specs ../specs --cases cases --registry coverage.yaml \
  --ecctl-bin bin/ecctl-public
```

New or changed operations must remain `offline` with `not-run` or
`case-changed`. Only a real cloud run of the current case bytes may change them
to `live-pass/live-verified`. Never hand-edit fingerprints or preserve
`live-pass` across a case-byte change.

## 8. Definition of done

Run all offline gates:

```bash
make lint
make validate
make validate-full
make test
```

Also run the repository-level tests required by the change and `git diff
--check`. A resource case is ready for review when:

- `resource_gaps` is empty;
- `gaps` is empty: every declared operation appears in a real case step;
- every canonical resource has exactly one primary lifecycle case;
- the case uses the correct surface and canonical resource;
- every template reference and stack dependency passes lint;
- every creatable prerequisite is fixture-owned and visible in the expanded
  dependency output;
- every mutation has an isolation and cleanup story;
- registry entries reference the case with current fingerprints and honest
  offline/live status;
- protected operations without prerequisites remain honest `offline` entries
  until their exact case bytes pass a real cloud run.

# Resource Reference API Calls

## Goal

Generated resource reference pages must show every OpenAPI call that an
`ecctl <product> <resource> <action>` operation can make. Each entry must state
the condition that triggers the call and its purpose. The list includes
preflight lookups, mutation calls, conditional workflow branches, waiter
polling, and final readback calls.

The machine-readable command schema remains the source of truth. The website
generator renders the schema instead of independently interpreting resource
specs.

## Output

Full command schemas gain an ordered `api_calls` array. Each item contains:

- `api`: the OpenAPI operation name;
- `phase`: `preflight`, `operation`, `wait`, or `readback`;
- `condition`: the machine-readable condition under which the call occurs;
- `condition_description`: the localized, CLI-oriented explanation shown to
  people in generated reference pages;
- `purpose`: a localized explanation of why the call is made;
- `repeated`: whether the API can be polled repeatedly;
- `cached`: whether a workflow readback reuses the preceding waiter result and
  therefore does not make another request.

Brief schemas do not include `api_calls`; the resource documentation generator
already requests `--full`. This preserves the existing size budget for schemas
used as compact Agent tools.

The generated English and Chinese pages add an API table to every action that
has calls. The table labels the condition column as **When called** / **调用时机**
and uses `condition_description` rather than exposing the condition DSL:

| API | When called | Purpose |
|---|---|---|
| `RunInstances` | Every time the command runs. | Create the ECS instance. |
| `DescribeInstances` | When `--no-wait` is not specified. | Wait for the target state. (repeated) |
| `DescribeInstances` | When `--no-wait` is not specified. | Return the final resource view by reusing the waiter result. (cached) |

Rows remain in execution order. The same API is not deduplicated when it has
different phases or purposes.

## Deriving the Call Sequence

The schema package walks the operation workflow in the same order as the
executor.

For each binding step it emits:

1. documented API calls performed by binding hooks;
2. the binding API itself;
3. the probe API used by a binding-level waiter, when waiting is enabled.

For each explicit wait step it emits the waiter's probe API. For each explicit
probe step it emits the probe API. Conditions from `when`, `when_any`, `unless`,
and `wait_unless` are combined rather than discarded. Binding waiters also
inherit the binding step's condition.

A probe immediately following a waiter can reuse the cached waiter result when
the probe and resolved ID set match. The schema represents that row with
`cached: true`, because it is part of the logical operation flow but does not
issue an additional API request in that path.

## Hook API Metadata

Most calls are directly available from bindings, probes, and waiters. A small
number of Go hooks make additional OpenAPI requests that cannot be inferred
from the workflow YAML. Binding hook metadata therefore gains an explicit,
documentation-only list of API calls. Each metadata entry identifies the hook,
API, phase, condition, and localized purpose.

The loader validates that a metadata entry refers to a hook attached to the
same binding. This keeps exceptional API calls beside the spec-driven binding
while preventing unattached documentation entries. The initial metadata covers
the existing API-calling hooks for ECS image-name resolution and tag associated
resource rule preservation. Hooks that only transform values do not add rows.

## Conditions and Localization

The schema keeps stable input-oriented conditions, such as
`input.type == ManagedKubernetes` and `!input.no_wait`, in `condition` for
Agents and diagnostics. The schema layer also produces a localized
`condition_description` that uses the command's real CLI flag or positional
name. For example, the ECS key-pair branches become **When `--public-key` is
specified** and **When `--public-key` is not specified**.

Condition descriptions preserve boolean grouping and turn runtime predicates
into user-facing language. They cover parameter presence, explicit empty
values, equality, single/multiple selectors, string prefixes, prefixed change
lists, workflow context, and the documented hook predicates. Negated compound
conditions are normalized before rendering so that, for example,
`!(input.no_wait || !has(input.entries))` is described as requiring both an
omitted `--no-wait` flag and a supplied `--entries` value.

All localized condition wording lives in `pkg/i18n`. The website generator does
not interpret condition syntax. If a non-empty condition has no description,
reference generation fails with the API and raw condition instead of silently
publishing the DSL. The raw `condition` remains available from
`ecctl schema ... --full` even though generated human documentation does not
show it.

Condition strings preserve conventional boolean grouping. When a workflow
condition containing `||` is combined with another requirement through `&&`,
the disjunction is wrapped explicitly, for example
`(has(input.name) || has(input.description)) && !(input.no_wait)`.

Waiter cache coverage is combined across the exhaustive
`single(input.ids)`/`multiple(input.ids)` selector pair only when the selected
input is required and non-empty and both branches use the same probe, IDs, and
additional conditions. Other conditional branches remain conservatively
uncached unless their cache condition exactly matches the following readback.

Generic purposes are derived from the phase and localized in the schema
package. Hook metadata supplies localized purpose text because the reason for
an imperative hook call is domain-specific. English and Chinese documents use
the same ordered schema data and only localize table headings, condition
phrasing, and purpose text.

An operation with no OpenAPI call produces no empty table. Invalid references
continue to fail spec loading or schema construction rather than silently
producing incomplete documentation.

## Testing

Tests are added before implementation and cover:

- a single-probe read operation;
- a mutation with its primary API, waiter polling, and cached final readback;
- conditional binding branches and their combined conditions;
- an API-calling hook and a non-API hook;
- full-schema inclusion and brief-schema omission;
- English and Chinese API tables, including conditions, purposes, repeated
  polling, and cached readback labels.

After the focused tests pass, regenerate all resource reference pages. Run the
relevant Go tests, the website generator tests, the full reference generation,
and the website build. Review representative simple, conditional, waiter, and
hook-backed resource pages in both languages.

## Non-goals

- Linking each API to external Alibaba Cloud documentation. Product-specific
  URL construction is not consistently derivable from the current specs.
- Reporting an exact runtime call count. Waiter counts depend on resource state
  and timing.
- Hand-maintaining API lists in generated Markdown.
- Changing resource execution behavior.

## Alternatives Considered

Reading resource YAML directly in the website generator would keep the Go
schema unchanged, but it would duplicate executor semantics in JavaScript and
eventually drift. Hand-authored API tables would allow arbitrary prose but
would be incomplete and expensive to maintain across the resource catalog.
Exposing the ordered call sequence through the existing full schema provides
one reusable contract for documentation and Agents.

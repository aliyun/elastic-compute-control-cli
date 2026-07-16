# Lingjun Public VPD Surface Design

Status: approved on 2026-07-13

## Context

The default public Lingjun command surface currently exposes `cluster`, `node`,
and `node-group`. The repository also contains a complete `vpd` resource spec
and focused tests, but the public CLI filter hides it.

The public surface must expose `cluster` and `vpd`. The node-related resources
must remain available to the separately selected full command surface, because
this change hides existing capabilities rather than deleting their specs or
implementation.

## Design

Change the Lingjun branch of `publicCLIResource` so that only `cluster` and
`vpd` pass the default public filter. This also removes the `ng` alias from the
public command router because alias acceptance delegates to the canonical
`node-group` visibility check. No new visibility abstraction is introduced.

Keep all entries in `specs/lingjun/product.yaml`, including `node` and
`node-group`, so full-surface discovery and internal resource tests continue to
cover them. Update only the product description and examples that are rendered
on the public surface so they describe clusters and VPDs without advertising
hidden commands. Regenerate the compiled catalog after this spec change.

Regenerate the English and Chinese resource reference pages from the rebuilt
public binary. The generated result must add `lingjun/vpd` and remove the
public `lingjun/node` and `lingjun/node-group` pages. Update the hand-maintained
English and Chinese coverage, quick-start, command-model, and Lingjun
optimization pages to show the same public surface. Internal interface design
notes under `docs/design/lingjun/` remain unchanged.

## Tests

Extend the existing public-surface test before changing production code. It
must prove that:

- `ecctl lingjun vpd --help` succeeds;
- `ecctl lingjun node --help`, `ecctl lingjun node-group --help`, and the `ng`
  alias fail on the public surface;
- public schema discovery lists exactly `cluster` and `vpd` for Lingjun; and
- full-surface execution still reaches the hidden node-related resources.

Existing focused VPD resource tests remain the execution-contract proof for
CreateVpd request mapping, asynchronous availability waiting, readback,
listing, updates, and deletion.

## Validation

Run the focused public-surface tests through a red-green cycle, then run
`make generate`, `make test`, and `make lint`. Rebuild the CLI, regenerate the
reference pages, and run the website tests, typecheck, and production build.
Inspect the resulting CLI schema/capabilities output and both language trees to
confirm that the runtime surface and documentation agree.

No live cloud calls or E2E evidence-status changes are required for this
visibility-only release.

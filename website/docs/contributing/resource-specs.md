---
title: Resource Specs
description: How contributors extend resource command coverage.
---

# Resource Specs

Cloud resource behavior is declared in YAML specs first. Go hooks are used only
when cross-API derivation or normalization cannot be expressed cleanly in the
spec schema.

## Layout

Product specs:

```yaml
specs/<product>/product.yaml
```

Resource specs:

```yaml
specs/<product>/<resource>.yaml
```

Example specs from this repository:

```yaml
specs/ecs/instance.yaml
specs/ecs/sg.yaml
specs/vpc/vpc.yaml
specs/vpc/vswitch.yaml
specs/ack/ack.yaml
```

## Generated Catalog

Resource specs are generated into:

```go
pkg/spec/catalog_generated.go
```

After changing specs, run:

```bash
make generate
```

Commit `pkg/spec/catalog_generated.go` when it changes.

## Reference Pages

The per-resource pages under **Reference → Resources by Product** are generated
from `ecctl schema`, not hand-written. After changing a spec, rebuild the binary
and regenerate them:

```bash
make build
npm --prefix website run gen:reference
```

Commit the regenerated Markdown under `website/docs/reference/resources/` and its
`zh-Hans` counterpart.

## Validation

Use the project targets first:

```bash
make test
make lint
```

For documentation-only changes under `website/`, also run:

```bash
cd website
npm run test
npm run typecheck
npm run build
```

## External Specs

Set `ECCTL_SPEC_DIR` to point discovery and command generation at an external
spec directory with the same layout:

```bash
ECCTL_SPEC_DIR=/path/to/specs ecctl schema --list
```

Use this for private experiments or validation before a spec is merged.

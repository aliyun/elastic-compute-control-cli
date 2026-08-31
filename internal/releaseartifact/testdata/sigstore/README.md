# Sigstore verification fixtures

`othername.sigstore.json` and `scaffolding-trusted-root.json` are copied from
`sigstore/sigstore-go` v1.3.0 at commit
`22d3691c7b8e0c5530fae3c05577690bfef5cd00`:

- `pkg/testing/data/bundles/othername.sigstore.json`
- `pkg/testing/data/trusted-roots/scaffolding.json`

They are used only to exercise the standardized bundle verifier, artifact
digest binding, and certificate identity policy. The upstream project is
licensed under Apache-2.0; see the repository's dependency license inventory.

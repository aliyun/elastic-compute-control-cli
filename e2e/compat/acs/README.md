# ACS compatibility E2E

This suite uses the public binary and a separate coverage registry. Its evidence
applies only to the selected ACS endpoint and manager version; it does not
promote native E2B or FC operations in `e2e/coverage.yaml`.

Configure `ECCTL_SANDBOX_BACKEND=acs`, `E2B_API_URL` (or `E2B_DOMAIN`),
`E2B_API_KEY`, and, for a private CA, `ECCTL_SANDBOX_CA_FILE`. Credentials stay
in the process environment and must never be added to this directory.

The suite discovers the first template by sorted ID through `template list`.
The target cluster must contain a ready SandboxSet with pause/resume and
checkpoint support. Use a test cluster: creation claims capacity from this
existing pool. The suite does not modify or delete the SandboxSet. No additional
account resource-ID configuration or Kubernetes client is needed by the cases.
Snapshot availability depends on the configured checkpoint driver.
Restoring a snapshot may exceed an ingress request timeout. Set the CLI timeout
and the ingress request/idle timeouts for the expected restore duration. A CLI
`--timeout 300s` cannot extend an ALB deadline. After an HTTP 504, check for
resources with the exact run ID before retrying. A successful port-forward run
validates the manager path only; record it separately from public HTTPS ingress
validation.

From the repository root, with the environment configured:

```sh
make -C e2e lint
e2e/bin/ecctl-e2e lint cases --config e2e/compat/acs/e2e.yaml
e2e/bin/ecctl-e2e run --config e2e/compat/acs/e2e.yaml --surface public --ecctl-bin e2e/bin/ecctl-public --collect-only --validate-binary
e2e/bin/ecctl-e2e run --config e2e/compat/acs/e2e.yaml --surface public --ecctl-bin e2e/bin/ecctl-public --report-dir e2e/reports/acs
e2e/bin/ecctl-e2e report check e2e/reports/acs/e2e-report.json --failed 0
e2e/bin/ecctl-e2e coverage registry check --specs specs --cases e2e/compat/acs/cases --registry e2e/compat/acs/coverage.yaml --ecctl-bin e2e/bin/ecctl-public --surface public
```

Initialize the ACS registry from its own directory so recorded case paths stay
relative to that registry:

```sh
cd e2e/compat/acs
../../bin/ecctl-e2e coverage registry init --specs ../../../specs --cases cases --registry coverage.yaml --ecctl-bin ../../bin/ecctl-public
```

The example region is cn-hongkong. Set `--region` to the actual cluster region
when running elsewhere; it records run/cleanup metadata, while sandbox API
requests remain global.
Only promote operations after checking the matching case fingerprint, assertions,
cleanup journal, and final API/Kubernetes readback. Unsupported commands belong
to CLI regression tests, not successful remote-operation coverage.

Every created sandbox and snapshot registers teardown immediately. The sandbox
TTL is bounded. If a run is interrupted, use its cleanup journal and the matching
binary/endpoint; never delete discovered templates or replay another run's
journal. Record the backend, non-secret endpoint, manager version, binary hash,
case hashes, run ID and cleanup result with each reviewed live report.

## Verified scope (2026-09-07)

Manager v0.6.6 was tested with public binary SHA-256
`887cb45df7f10346d20330941a2d79d0c7ff632ee0df2ce97048c42a09412532`.
The registry records 11 supported operations across the two resources. It does
not claim support for all parameters or for commands rejected by the ACS profile.

| Run ID | Transport | Result |
| --- | --- | --- |
| acs-adapt-20260907 | Public HTTPS through a loopback CONNECT tunnel; ecctl verified the server certificate and hostname | All 13 sandbox lifecycle steps passed. Template discovery, source creation and snapshot passed; snapshot restore received ALB HTTP 504 after 60 seconds. |
| acs-adapt-snapshot-20260907 | Kubernetes port-forward to sandbox-manager:8080 | All 11 template lifecycle steps passed; restore took 76 seconds and source deletion waited 49 seconds for API absence. |

The CONNECT tunnel carried encrypted traffic without terminating TLS; it worked
around an intermittent local process TCP connection failure during this run.
The port-forward result does not establish successful snapshot restore through
the public HTTPS listener. That path still requires ingress timeout adjustment
and a new validation run. No shared listener configuration was changed.

Both runs ended with no owned sandboxes in the API and no matching Sandbox, Pod,
Checkpoint or SandboxTemplate objects in Kubernetes. Case SHA-256 values were
`9963b0f782594e5852bc4c8d17224e70cb55746a46abf4827e852ff422a92922`
(sandbox lifecycle) and
`d55ad858b0eaa875533ece81593d5411b34381ad595b00f51641b9b2ad14be41`
(template lifecycle).

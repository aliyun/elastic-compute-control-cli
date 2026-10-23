# sandbox template-cache

`ecctl sandbox template-cache` and `ecctl sbx template-cache` manage an FC
Sandbox template's image warming cache. This resource uses Aliyun profile AK/STS
credentials, a resolved target region, and FCSandbox `2026-05-09` ROA. It does
not use the E2B API key or change the default `sandbox` and `sandbox template`
routes. Native templates are managed with `sandbox template --openapi`; create a
Template Cache only after that template's `build_status` is `ready`.

| CLI action | FC API and ROA path | Scope |
|---|---|---|
| `create` | `POST /pop/2026-05-09/template-caches` (`CreateTemplateCache`) | JSON `teamID`, `templateID` |
| `get` | `GET /pop/2026-05-09/template-caches/{templateID}` (`DescribeTemplateCache`) | query `teamID` |
| `list` | `GET /pop/2026-05-09/template-caches` (`ListTemplateCache`) | query `teamID`, `templateID`, `status`, `maxResults`, `nextToken` |
| `delete` | `DELETE /pop/2026-05-09/template-caches/{templateID}` (`DeleteTemplateCache`) | query `teamID` |

The cache has no separate ID: output `id` and `template_id` both contain the
FC `templateID`. Provide `--team-id` to keep an operation in the intended Team.
The API metadata does not mark Team ID mandatory, so the CLI also permits the
service's default Team behavior. It requires a region from `--region`, the
environment, or the selected profile, and uses
`fcsandbox.<region>.aliyuncs.com`. Cache state and capacity are regional.

```bash
ecctl sbx template-cache create --template-id <template-id> --team-id <team-id> --region cn-beijing
ecctl sbx template-cache get <template-id> --team-id <team-id> --region cn-beijing
ecctl sbx template-cache list --team-id <team-id> --template-id <template-id> --limit 20 --region cn-beijing
ecctl sbx template-cache delete <template-id> --team-id <team-id> --region cn-beijing
```

## `ecctl sandbox template-cache create`

Create is asynchronous at the API. The command waits by default, probing
`DescribeTemplateCache` every 15 seconds for up to 20 minutes. Only cache
`status=Success` means warming succeeded. The provider also returns lower-case
`ready` for this state; the CLI reports it as `Success` before evaluating the
wait. Create and Describe can return lower-case `creating`; the CLI reports it
as `InProgress` and keeps waiting. `Failed` and `Evicted` fail the wait;
`InProgress` and `Deleting` continue until success or timeout. An initial
not-found read is treated as pending because the accepted task may not yet be
visible. `--no-wait` returns the accepted cache object, including its current
status, without claiming readiness. If the command fails after acceptance,
use `get` to determine whether a cache exists before repeating `create`.
The template's own build status is never used as the cache state. `progress`
is display data, not a readiness signal.

## `ecctl sandbox template-cache delete`

Delete waits by default until `DescribeTemplateCache` explicitly reports
`TemplateCacheNotFound` or HTTP 404. This confirms logical deletion before
deleting the underlying template. `--no-wait` reports only
`deletion_requested=true`; a successful DELETE response alone does not prove
absence. `TemplateNotFound` and 404 responses from operations other than
`DescribeTemplateCache` cannot certify cache deletion. Do not delete a
business template as part of cache cleanup.

## `ecctl sandbox template-cache get`

Get exposes `status`, `status_reason`, `progress`, `image_digest`,
`image_size_mb`, `created_time`, and `ready_time`. The 2026-05-09 contract has
five canonical case-sensitive cache states: `InProgress`, `Success`, `Failed`,
`Deleting`, and `Evicted`. A provider `ready` response is normalized to
`Success`, and Describe `creating` to `InProgress`; other unknown states remain
invalid. `status_reason` may be empty.

## `ecctl sandbox template-cache list`

List exposes the same cache fields. It defaults to 100 per page and
accepts 1–100. Its `--next-token` is the service's opaque token; a nonempty
response token is the only signal that another page exists. The CLI does not
translate it into a local cursor or infer completion from a short page.

The provider rejects HTTP-success envelopes unless lower-case `code` is the
string `"200"`. Business failures keep sanitized `message`, `code`, and
`requestId`. Successful cache objects must match the requested Team and
Template, have a valid cache state and typed optional fields. Lists must be
arrays with unique Team/Template pairs and a string `nextToken`; invalid
responses fail instead of becoming empty results. Service errors such as
`TemplateNotReady`, `TemplateCacheAlreadyExists`, and capacity limits remain
visible with their request IDs. Recreating an evicted or failed cache requires
deleting the old cache and confirming absence first.

The independent E2E case `e2e/cases/sandbox/template-cache-lifecycle.yaml`
creates a run-specific Team and fully built native Template from the private
`sandbox.template.runtime_config_file` fixture, then creates, reads, lists and
deletes its cache before deleting Template and Team. Teardown and the
run-specific cleanup journal handle interrupted runs. If a create response is
lost or does not confirm a ready cache, its journal entry remains pending;
unattended replay cannot consume the delete or its parent finalizers until an
operator reconciles the cache's actual state. An immediate not-found read does
not prove that an accepted create will never appear. The four registry
operations are `live-pass` for the exact authorized lifecycle case recorded in
`e2e/coverage.yaml`; default tests do not call FC. The API basis is the
[official 2026-05-09 metadata](https://api.aliyun.com/meta/v1/products/FCSandbox/versions/2026-05-09/api-docs.json)
(SHA-256 `3f02e1d7cac1670429d93dce730907340d1f0d6faefa777f53229428616df11c`).
The newer 2026-08-20 guide uses different state spellings and is not this
resource's wire contract.

For a run-owned pending Cache finalizer, `ecctl-e2e sweep
reconcile-template-cache JOURNAL` is an explicit step before `sweep replay`.
It requires the original run, region, surface, profile, journal SHA-256,
original delete binary path and SHA-256, and a separately pinned probe binary
path and SHA-256. Under the replay lock it validates the native Template
creation intent and current STS account, then uses the probe binary for a
read-only Cache get. Only an exact Team/Template match in `Success`, `Failed`,
or `Evicted` clears the pending marker. It first writes a private durable
reconciliation record next to the journal, then durably updates that one
marker. A missing or still-running cache retains the marker. The command
performs no delete; the ordinary replay command still validates and executes
the reverse-order finalizers.

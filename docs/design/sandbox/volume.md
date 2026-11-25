# sandbox volume

`ecctl sandbox volume` and `ecctl sbx volume` manage regional FCSandbox Volume
configurations through the `2026-05-09` ROA API. They use the selected Aliyun
profile's AK/STS credential and the resolved `fcsandbox.<region>.aliyuncs.com`
endpoint. The default `sandbox` resource and its `volume_mounts` input remain
on the existing E2B route; `volume` is an explicit FC OpenAPI subresource.

| Command | API and path | Scope and input |
|---|---|---|
| `create` | `CreateVolume`, `POST /pop/2026-05-09/volumes` | JSON body `volumeName`, optional `teamID`, storage configuration and `mountConfig` |
| `delete` | `DeleteVolume`, `DELETE /pop/2026-05-09/volumes/{volumeID}` | path `volumeID`, optional query `teamID` |
| `get` | `GetVolume`, `GET /pop/2026-05-09/volumes/{volumeID}` | path `volumeID`, optional query `teamID` |
| `list` | `ListVolumes`, `GET /pop/2026-05-09/volumes` | query filters and token pagination |
| `update` | `UpdateVolume`, `PUT /pop/2026-05-09/volumes/{volumeID}` | path `volumeID`, JSON body `status` and `teamID` |

Create requires a `--name` because `volumeName` is unique within a Team.
`--team-id` is optional in the published request schema, but specify it when
working with a particular Team. Storage and mount inputs take JSON or `@file`:
`--oss-volume-config`, `--agentic-fs-volume-config`,
`--agentic-bucket-volume-config`, and `--mount-config`. The last object can
contain `role` and `vpcConfig` with `vpcId`, `vSwitchIds`, and
`securityGroupId`. The CLI passes combinations to the service; it does not
invent a storage class or a rule that the published schema does not define.

```bash
ecctl sandbox volume create --name workspace --team-id <team-id> \
  --oss-volume-config @oss.json --mount-config @mount.json --region cn-hangzhou
ecctl sandbox volume get <volume-id> --team-id <team-id> --region cn-hangzhou
ecctl sandbox volume list --team-id <team-id> --name workspace --limit 100 --all --region cn-hangzhou
ecctl sandbox volume delete <volume-id> --team-id <team-id> --region cn-hangzhou
ecctl sandbox volume update <volume-id> --team-id <destination-team-id> --region cn-hangzhou
```

## `ecctl sandbox volume create`

Create waits by default for `status=AVAILABLE`, probing `GetVolume` every two
seconds for up to five minutes. `CREATING` and an initial not-found read are
pending; `ERROR` fails with `statusReason`. `--no-wait` returns the accepted
Volume's ID and current status without claiming readiness. If waiting fails
after CreateVolume was accepted, inspect the Volume before retrying create.

## `ecctl sandbox volume update`

Update accepts only `--status` and `--team-id`; at least one is required. It
does not update the volume name or storage backend configuration. Setting
`--team-id` transfers the volume to that Team. After `UpdateVolume` is
accepted, the CLI reads the volume back with `GetVolume` in the destination
Team scope (the Team returned by `UpdateVolume` when `--team-id` is omitted)
and returns that readback. A business `code` other than `"200"` fails the
command; the readback preserves the sanitized `requestId` and the provider
volume fields. A scoped readback does not prove the volume is absent from its
previous Team.

## `ecctl sandbox volume delete`

Delete waits by default for a scoped `GetVolume` to report the Volume-specific
`VolumeNotFound` code. A generic HTTP or business `404` does not prove absence.
`DELETING` remains pending and `ERROR` fails the wait. `--no-wait` reports
`deletion_requested=true`; the DELETE envelope alone does not establish
absence. A successful scoped `GetVolume` absence check reports
`absent_in_scope=true`. It does not prove terminal absence after a possible
Team transfer.
The published `synchronous` marker does not establish either readiness or
absence.

## `ecctl sandbox volume get`

`get` returns `id`, `name`, `team_id`, `resource_group`, `storage_class`, the
storage and mount configuration objects, `created_at`, `updated_at`,
`user_id`, `status`, and `status_reason` when supplied by the service.

## `ecctl sandbox volume list`

`list` returns the same Volume fields. Filters map `--name`, `--status`,
`--team-id`, `--resource-group`, and `--user-id` to `volumeName`, `status`,
`teamID`, `resourceGroupID`, and `userID`. `--limit` maps to `maxResults` and
defaults to 100. `--next-token` is an opaque service token. `--all` follows
token pages from the beginning, preserves filters, and rejects repeated
tokens or Volume IDs. It does not provide a consistent snapshot of concurrent
changes.

All five operations require lower-case `code="200"` inside the response even
when HTTP succeeds. Business errors preserve sanitized code, message, and
request ID. Invalid success envelopes and malformed Volume or list objects
fail instead of becoming successful empty results.

For an FC sandbox mount, use the returned Volume `name` in `volume_mounts` as
`{"/mnt/oss": "<volume.name>"}`: the key is the mount directory and the value
is the Volume name, not its ID. Use an API key bound to the same Team as the
Volume and select the matching region. See the [FC OSS Volume mount guide](https://help.aliyun.com/zh/functioncompute/mount-oss-volume).
The returned `id` is the `volumeID` for Volume management operations such as
get and delete. The default `sandbox create --volume-mount` passes
the supplied mount configuration to its E2B backend; the CLI does not resolve
a Volume ID to its name. `DeleteVolume` removes the Volume
configuration; the published API does not state that it deletes underlying
OSS or AgenticFS data. This command neither modifies nor cleans that storage.

The authored public E2E case `e2e/cases/sandbox/volume-lifecycle.yaml` uses
two case-owned Teams, three Volumes, and a read-only OSS configuration derived
from execution-scoped fixtures. It verifies token pagination and exact
`VolumeNotFound` readback under the original Team before retiring each
Volume finalizer. A third Volume exercises `UpdateVolume` team transfer: the
runner reserves the destination scope and a pending transfer marker before the
provider call, observes the Volume in the destination Team under its original
credential, then re-anchors the retained creation intent and cleanup finalizer
to that destination Team so the single-Team absence and retirement primitives
apply unchanged. In the dedicated test account, cleanup uses the recorded
profile, Hangzhou region, and run-assigned OSS Bucket and RAM fixture names.
Uncertain outcomes retain the journal.
Volume has no tag-based sweep contract. The five case operations (`create`,
`get`, `list`, `update`, and `delete`) are tracked in `e2e/coverage.yaml`
against the public case fingerprint, the SHA-256 of
`e2e/cases/sandbox/volume-lifecycle.yaml`
(`sha256:8bad342fb00bc230554b136bed3670f90613b6480f343d1c4b0c7d9821ce27f4`).
Adding the transfer steps changed the case fingerprint, so all five operations
are `offline` with reason `case-changed` pending live re-verification under the
new fingerprint; a case change requires live re-verification of every operation
it covers. The repository admits exactly one lifecycle case per resource, so
the transfer lives in the same lifecycle case rather than a separate file.
Offline unit tests cover request construction, nested JSON, business errors,
wait states, no-wait, scope, token pagination, and the update transfer
readback.

Source: [official FCSandbox 2026-05-09 API metadata](https://api.aliyun.com/meta/v1/products/FCSandbox/versions/2026-05-09/api-docs.json),
[CreateVolume](https://api.aliyun.com/document/FCSandbox/2026-05-09/CreateVolume),
[GetVolume](https://api.aliyun.com/document/FCSandbox/2026-05-09/GetVolume),
[ListVolumes](https://api.aliyun.com/document/FCSandbox/2026-05-09/ListVolumes), and
[DeleteVolume](https://api.aliyun.com/document/FCSandbox/2026-05-09/DeleteVolume), and
[UpdateVolume](https://api.aliyun.com/document/FCSandbox/2026-05-09/UpdateVolume).

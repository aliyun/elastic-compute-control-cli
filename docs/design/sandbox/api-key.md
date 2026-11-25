# sandbox api-key

`ecctl sandbox api-key` (alias `ecctl sandbox apikey`, product alias `sbx`)
manages regional FCSandbox API keys through the `2026-05-09` ROA API. It uses
the selected Aliyun profile's AK/STS credential and the resolved
`fcsandbox.<region>.aliyuncs.com` endpoint. The managed E2B-compatible API key
is a resource, not an OpenAPI credential: these commands never use it to sign
requests and never rewrite the user's credential configuration. The default
`sandbox` resource and E2B template operations remain on their existing routes;
`api-key` is an explicit FC OpenAPI subresource.

| Command | API and path | Scope and input |
|---|---|---|
| `create` | `CreateApiKey`, `POST /pop/2026-05-09/api-keys` | JSON body `apiKeyName`, optional `teamID`, `expireTime`, `ipWhitelist`, `ipBlacklist` |
| `update` | `UpdateApiKey`, `PUT /pop/2026-05-09/api-keys/{apiKeyID}` | path `apiKeyID`, JSON body name, `status`, `expireTime`, IP rules |
| `delete` | `DeleteApiKey`, `DELETE /pop/2026-05-09/api-keys/{apiKeyID}` | path `apiKeyID`; an active key is first deactivated with `UpdateApiKey` |
| `get` | `DescribeApiKey`, `GET /pop/2026-05-09/api-keys/{apiKeyID}` | path `apiKeyID` |
| `list` | `ListApiKeys`, `GET /pop/2026-05-09/api-keys` | query filters and page pagination |
| `reset` | `ResetApiKey`, `PUT /pop/2026-05-09/api-keys/{apiKeyID}/reset` | path `apiKeyID` |

`apiKeyID` is the resource identity (`id`). Create requires `--name`; `--team-id`
is optional in the published request schema but should be set when working with
a particular Team. IP rules are object arrays: repeat `--ip-whitelist` or
`--ip-blacklist` with one JSON object (or `@file`) per rule, each carrying
`ipAddress` and optional `description`.

```bash
ecctl sandbox api-key create --name dev --team-id <team-id> --region cn-hangzhou
ecctl sandbox api-key get <api-key-id> --region cn-hangzhou
ecctl sandbox api-key list --team-id <team-id> --status active --limit 50 --page 1 --region cn-hangzhou
ecctl sandbox api-key update <api-key-id> --status inactive --region cn-hangzhou
ecctl sandbox api-key reset <api-key-id> --show-value --region cn-hangzhou
ecctl sandbox api-key delete <api-key-id> --region cn-hangzhou
```

## `ecctl sandbox api-key create`

Create returns the new key. The plaintext `apiKeyValue` is a secret: by default
the caller strips it at the provider boundary and the output carries only
`api_key_mask`. `--show-value` explicitly requests the plaintext, which is then
returned once in `api_key_value`. The reveal control is consumed by the caller
and never sent to the service.

## `ecctl sandbox api-key update`

Update changes the name, expiration, IP rules, or `status` (`active` or
`inactive`); at least one mutable field is required. The CLI reads the key back
with `DescribeApiKey` after the update. The read-back always masks the plaintext.

## `ecctl sandbox api-key delete`

The service only deletes inactive keys: `DeleteApiKey` on an active key fails
with `InvalidApiKeyStatus`. Delete therefore reads the key with `DescribeApiKey`
first and, when the key is `active`, issues `UpdateApiKey` with `status=inactive`
before `DeleteApiKey`. An already inactive key is deleted without the extra
update. Delete then waits by default for a `DescribeApiKey` read to report the
key-specific `ApiKeyNotFound` code (or business `404`). `--no-wait` reports
`deletion_requested=true`; the DELETE envelope alone does not establish absence.
A successful absence check reports `deleted=true`. Every issued call is recorded
in `actions`.

## `ecctl sandbox api-key get`

`get` returns `id`, `name`, `team_id`, `team_name`, `team_plan`,
`resource_group`, `user_id`, `username`, `source`, `status`, `created_time`,
`expire_time`, `last_used_time`, `api_key_mask`, and the `ip_whitelist` /
`ip_blacklist` rule arrays when supplied by the service. The server-reported
`permission_policy` and `effective_permissions` objects are preserved verbatim
alongside the key. The plaintext is always masked on get.

## `ecctl sandbox api-key list`

`list` returns the same key fields (always masked). Filters map `--name`,
`--status`, `--team-id`, `--resource-group`, and `--user-id` to `apiKeyName`,
`status`, `teamID`, `resourceGroupID`, and `userID`. `--limit` maps to
`pageSize` (default and CLI cap 50), `--page` maps to `pageNumber`, and `total`
drives pagination. `--all` follows pages from page 1 and preserves filters.

## `ecctl sandbox api-key reset`

Reset generates a new key value. Like create, the plaintext is hidden by default
and shown only with `--show-value`. The previous value is invalidated by the
service; the CLI does not retain it. Reset also reactivates the key: the response
reports `status=active` with a new `api_key_mask` even when the key was inactive
before the reset. A later delete deactivates it again automatically.

All six operations require lower-case `code="200"` inside the response even when
HTTP succeeds. Business errors preserve sanitized `code`, `message`, and
`requestId`; an HTTP success with a failed business code never reports success.
Invalid success envelopes and malformed key or list objects fail instead of
becoming successful empty results. The plaintext key is scrubbed from get, list,
update, and delete responses unconditionally, and from create/reset responses
unless explicitly revealed, so table, JSON, log, and error output do not leak it.

Relationship to `E2B_API_KEY`: the value returned by create/reset is an
E2B-compatible key bound to a Team. Export it as `E2B_API_KEY` (or pass it to an
E2B client) to use the E2B data plane; it is unrelated to the Aliyun AK/STS
credential that signs these management requests. See the sandbox product help
for the E2B route selection.

The authored public E2E case `e2e/cases/sandbox/api-key-lifecycle.yaml` uses one
case-owned Team on the default plan (CreateApiKey has no plan constraint, and an
new Team ships platform templates that block `DeleteTeam`) and exercises
create, get, list (page pagination), update (inactive), reset (reactivation with
a new mask), and delete with read-back absence. Delete covers both branches: the
reset key is deleted while active (automatic deactivation) and the second key is
deleted after an explicit update to inactive. API keys have no tag-based sweep
contract; cleanup uses lifecycle teardown and the run-specific cleanup journal.
The case opts into `cleanup_provider_templates`: teardown and journal replay
inventory and delete the platform templates under the owned Team, prove the
empty list, and then delete the Team, retrying the brief
`Conflict.TeamHasApiKeys` window after the last key is confirmed absent. The six
operations (`create`, `get`, `list`, `update`, `delete`, and `reset`) are
`live-pass` in `e2e/coverage.yaml`, recorded on 2026-11-25 with reason
`live-verified`. The record is bound to the matching public case fingerprint,
the SHA-256 of `e2e/cases/sandbox/api-key-lifecycle.yaml`
(`sha256:7cc065bacd63340da4dad8658263a2a6bdc073af43baf07afa3ca40e27f2429e`);
a case change requires live re-verification under the new fingerprint.
Offline unit tests cover request construction, page pagination, business errors,
absence, the delete deactivation branches, and the default-mask / explicit-reveal
boundary.

Source: [official FCSandbox 2026-05-09 API metadata](https://api.aliyun.com/meta/v1/products/FCSandbox/versions/2026-05-09/api-docs.json),
[CreateApiKey](https://api.aliyun.com/document/FCSandbox/2026-05-09/CreateApiKey),
[DescribeApiKey](https://api.aliyun.com/document/FCSandbox/2026-05-09/DescribeApiKey),
[ListApiKeys](https://api.aliyun.com/document/FCSandbox/2026-05-09/ListApiKeys),
[UpdateApiKey](https://api.aliyun.com/document/FCSandbox/2026-05-09/UpdateApiKey),
[DeleteApiKey](https://api.aliyun.com/document/FCSandbox/2026-05-09/DeleteApiKey), and
[ResetApiKey](https://api.aliyun.com/document/FCSandbox/2026-05-09/ResetApiKey).

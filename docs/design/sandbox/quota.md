# sandbox quota

`ecctl sandbox quota` and `ecctl sbx quota` manage a team's regional FCSandbox
quota configuration. The resource uses Aliyun profile AK/STS credentials and
the resolved `fcsandbox.<region>.aliyuncs.com` endpoint. Existing E2B sandbox
and default template commands keep their E2B credentials and routing.

| Command | FCSandbox 2026-05-09 ROA | Identifier and input |
|---|---|---|
| `get <team-id>` | `DescribeQuota`, `GET /pop/2026-05-09/quotas/tag` | query `tagValue=<team-id>` |
| `list` | `ListQuota`, `GET /pop/2026-05-09/quotas` | query `maxResults`, `nextToken` |
| `update <team-id>` | `UpdateQuota`, `PUT /pop/2026-05-09/quotas/tag` | JSON body `tagValue`, `cpuCores`, `memoryGB` |
| `delete <team-id>` | `DeleteQuota`, `DELETE /pop/2026-05-09/quotas/tag` | query `tagValue=<team-id>` |

`tagValue` is the Team ID, exposed as a positional `<team-id>` on get, update,
and delete. Both `--cpu-cores` and `--memory-gb` are required for update.
CPU is measured in cores; memory is measured in **GB**, unlike template memory
settings expressed in MB. There is no standalone quota create API in this
version. Update can configure a quota for a Team that has no current quota.
Delete removes the quota configuration, not the Team.

```bash
ecctl sandbox quota list --limit 10 --region cn-beijing
ecctl sandbox quota list --next-token <token> --region cn-beijing
ecctl sandbox quota list --all --region cn-beijing
ecctl sandbox quota get <team-id> --region cn-beijing
ecctl sandbox quota update <team-id> --cpu-cores 32 --memory-gb 64 --region cn-beijing
ecctl sandbox quota delete <team-id> --region cn-beijing
```

The CLI defaults `--limit` to 100 and caps it at 100; the published API schema
does not state a service-side maximum. `list` returns `quotas` and the server's
opaque `nextToken` as CLI pagination metadata. Do not treat it as a page number.
`--all` follows up to 100 token pages from the beginning, including empty
pages with a continuation token, rejects repeated tokens or quota IDs, and
returns `pagination.has_more=false` when complete. Empty continuation pages
are accepted by the schema; their live service behavior remains unverified.
A successful HTTP status is not sufficient: all four operations require
lower-case `code="200"`. Non-success
`code`, `message`, and `requestId` are preserved as service error evidence;
malformed successful quota objects and list pages fail. A successful
`DeleteQuota` returns only the business envelope and is documented as
synchronous. After validating that envelope, ecctl polls read-only
`DescribeQuota` for the same Team ID, at most five minutes, before marking
the quota configuration deleted. `ResourceQuotaNotFound` is recognized only
for `DescribeQuota`; its provider code, message, and request ID remain in the
action evidence. The E2E case then makes a terminal read and expects NotFound
with a provider code and request ID. The server's actual post-delete response
still requires live observation.

Offline tests cover query/body placement, units, token mapping, business
failures, malformed responses, and the four resource workflows. The E2E case
`e2e/cases/sandbox/quota-lifecycle.yaml` creates a unique Team before changing
its quota. The list step traverses token pages and requires that Team's quota
in the merged result. It verifies a post-delete read before deleting the Team.
Its cleanup journal only deletes that Team's quota and Team; it must not delete
an existing business quota.

Source: [official FCSandbox 2026-05-09 API metadata](https://api.aliyun.com/meta/v1/products/FCSandbox/versions/2026-05-09/api-docs.json),
[DescribeQuota](https://api.aliyun.com/document/FCSandbox/2026-05-09/DescribeQuota),
[ListQuota](https://api.aliyun.com/document/FCSandbox/2026-05-09/ListQuota),
[UpdateQuota](https://api.aliyun.com/document/FCSandbox/2026-05-09/UpdateQuota), and
[DeleteQuota](https://api.aliyun.com/document/FCSandbox/2026-05-09/DeleteQuota).

## `ecctl sandbox quota update`

Set both CPU cores and memory GB for a Team ID; return the validated quota
from UpdateQuota without a second read.

## `ecctl sandbox quota delete`

Remove only the identified quota configuration after a successful business
envelope and bounded read-only absence confirmation. The Team itself remains.

## `ecctl sandbox quota get`

Read the specified Team ID through the query `tagValue`.

## `ecctl sandbox quota list`

Read one token page at a time through `maxResults` and `nextToken`, or use
`--all` to collect up to 100 pages.

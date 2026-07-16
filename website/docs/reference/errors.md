---
title: Error Model
description: Structured error fields, categories, and exit behavior.
---

# Error Model

`ecctl` writes errors as structured JSON to `stdout`, so a caller parses a
failure the same way it parses a result. The command exits non-zero on error.
The conventions on this page are reported by `capabilities`:

```bash
ecctl capabilities --output json
```

## Error Fields

A failure is reported under an `error` object. The field set is:

| Field | Meaning |
|---|---|
| `kind` | Error category (see below) |
| `code` | Stable error code |
| `message` | User-facing message |
| `retryable` | Whether retrying the command is appropriate |
| `suggestion` | Human-readable suggestion |
| `suggested_action` | Machine-oriented next action |
| `field` | Related input field, when applicable |
| `accepted_values` | Valid values for `field`, when available |

When a command issues one or more Alibaba Cloud API calls, each call is also
reported under `actions` with `action_name`, `code`, and `message`, so the
originating `request_id` and cloud error are preserved alongside the normalized
`error`.

## Error Categories

`kind` groups failures by origin:

- `client` — the request is invalid before any cloud call, such as an unknown
  schema or a missing required parameter. Not retryable as-is.
- `not_found` — the addressed resource does not exist.
- `service` — an Alibaba Cloud API call failed. The `actions` entries carry the
  per-call `request_id`, `code`, and `message`.

## Examples

An unknown schema is a `client` error:

```bash
ecctl schema ecs.instance.frobnicate --brief
```

```json
{
  "error": {
    "kind": "client",
    "code": "UnknownSchema",
    "message": "schema command is not supported",
    "retryable": false,
    "suggestion": "Run `ecctl schema --list` to list supported schemas.",
    "suggested_action": "Run `ecctl schema --list` to list supported schemas."
  }
}
```

A missing required parameter is also a `client` error and names the parameters:

```bash
ecctl ecs instance create --region cn-hangzhou
```

```json
{
  "error": {
    "kind": "client",
    "code": "MissingParameter",
    "message": "missing required parameters: --image, --sg, --type, --vswitch",
    "retryable": false,
    "suggestion": "Run the command with `--help` to see required parameters."
  }
}
```

Reading a resource that does not exist is a `not_found` error:

```json
{
  "error": {
    "kind": "not_found",
    "code": "NotFound",
    "message": "vpc not found",
    "retryable": false
  }
}
```

## Handling Errors in Automation

Use JSON output, branch on `error.kind` and `error.code`, and consult
`error.retryable` before retrying. When present, `error.field` and
`error.accepted_values` identify exactly which input to correct.

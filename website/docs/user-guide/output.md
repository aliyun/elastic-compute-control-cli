---
title: Output, Language, and Errors
description: Select output modes, languages, and structured error handling.
---

# Output, Language, and Errors

`ecctl` is JSON-first. Use `text` only when a human is reading command output.

## Output Mode

JSON output:

```bash
ecctl capabilities --output json
```

Text output:

```bash
ecctl capabilities --output text
```

Force JSON regardless of configured defaults:

```bash
ecctl --json capabilities
```

Wrap JSON in the Agent envelope:

```bash
ecctl --agent-envelope capabilities
```

## Language

Help and user-facing messages support English and Simplified Chinese:

```bash
ecctl --lang en --help
ecctl --lang zh-CN --help
```

Persist the language preference:

```bash
ecctl configure set lang zh-CN
```

Scripts that assert exact text should pass `--lang` explicitly.

## Structured Errors

Capabilities report that structured errors are written to `stdout` and can
include these fields:

| Field | Meaning |
|---|---|
| `kind` | Error category |
| `code` | Stable error code |
| `message` | User-facing message |
| `retryable` | Whether retry is appropriate |
| `suggestion` | Human-readable suggestion |
| `suggested_action` | Machine-oriented next action |
| `field` | Related input field |
| `accepted_values` | Valid values when available |

Use JSON output when automating error handling. See the
[Error Model](../reference/errors.md) reference for error categories and exit
behavior.

## Color

Disable color in human-readable output:

```bash
ecctl --no-color --output text capabilities
```

Set `ECCTL_DISPLAY_MODE=AI` or `ECCTL_DISPLAY_MODE=agent` for compact,
non-highlighted output that is easier for agents to parse. Set
`ECCTL_DISPLAY_MODE=Human` for human display: pretty JSON and highlighted text.
The value is case-insensitive, so `ai`, `Agent`, and `AGENT` work too.

By default, `ECCTL_DISPLAY_MODE` is `auto`: terminal output uses `Human`, while
non-terminal output such as pipes, redirects, and CI logs uses `AI`.

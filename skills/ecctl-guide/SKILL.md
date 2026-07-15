---
name: ecctl-guide
description: "Provides the ecctl agent usage protocol: how to discover commands with help/schema/capabilities, choose safe defaults, avoid polling loops, and plan Alibaba Cloud ECS, VPC, ACK, Lingjun, resource-group, or tag operations through ecctl."
---

# ecctl Agent Protocol

Use this skill when choosing or executing `ecctl` commands.

## Discovery Order

1. Run `ecctl <product> <resource> <action> --help` to see usage, examples, and available flags.
2. Use `ecctl schema <cmd1> <cmd2> ...` to batch-query multiple schemas in one call.
3. Use `ecctl schema <product>.<resource>.<action> --full` only when a needed advanced flag is absent from brief schema.
4. Use `ecctl capabilities --output json` for the full product/resource/action surface.

## Agent Rules

- Default output is JSON; parse stdout, not prose.
- Errors are structured JSON on stdout with `error.code`, `error.message`, and optional `suggestion` / `suggested_action`.
- Create/update/delete actions usually wait and read back state; do not write polling loops unless schema says the command is not waitable or `--no-wait` was used.
- For mutation commands, check `contract.dry_run`, `contract.idempotency`, and `contract.wait` before executing.
- Prefer `--filter key=value` for list filters; tag filters use `--filter tag.<key>=<value>`.
- **IDs are positional arguments**, not flags. Use `ecctl ecs sg authorize <sg-id>`, not `--id <sg-id>`.
- Use `--api-param key=value` only as an escape hatch when schema exposes it.
- When creating VSwitch, use `ecctl ecs zone list` to check zone resource creation support.
- When creating ECS instances, use `ecctl call ecs DescribeAvailableResource --DestinationResource InstanceType` before choosing zone/type/disk combinations. If `ecs instance create` returns a stock or disk-category error, follow its `error.suggested_action`.
- ecctl output uses **normalized field names** (e.g. `id`, not `zone_id` or `ZoneId`). Always check `--help` or actual output to confirm field names before parsing.

## Product And Resource Surface

- `ack`: `ack`, `addon`, `alert`, `alert-contact`, `alert-contact-group`, `audit`, `audit-control-plane-log`, `auto-repair-policy`, `check`, `diagnosis`, `diagnosis-check-item`, `event`, `inspect`, `inspect-config`, `inspect-report`, `kubeconfig`, `node`, `nodepool`, `operation-plan`, `permission`, `policy`, `policy-instance`, `region`, `task`, `template`, `trigger`, `version`, `vuls`
- `ecs`: `instance`, `disk`, `image`, `sg`, `eni`, `keypair`, `snapshot`, `snapshot-group`, `auto-snapshot-policy`, `command`, `launch-template`, `prefix-list`, `port-range-list`, `region`, `zone`, `assistant`
- `vpc`: `vpc`, `vswitch`
- `lingjun`: `cluster`, `node`, `node-group`, `vpd`, `subnet`, `vcc`, `er`, `eni`, `lni`, `vsc`, `net-test`
- `rg`: `group`, `resource`, `policy`, `policy-version`, `role`, `service-linked-role`, `associated-transfer`, `admin-setting`, `notification`
- `tag`: `resource`, `policy`, `associated-resource-rule`

---
title: ECS RAM role
description: Obtain renewable credentials from the instance metadata service on an ECS instance.
---

# ECS RAM role

`EcsRamRole` obtains temporary credentials from the ECS instance metadata
service (IMDS) for a RAM role attached to the instance. Nothing secret is
stored on disk, and the credentials renew themselves, which makes this the
right default for workloads already running on ECS.

This mode only works on an ECS instance whose role is attached. Running it
elsewhere fails when the metadata service is unreachable.

## Configure with Alibaba Cloud CLI

```bash
aliyun configure --mode EcsRamRole --profile instance
```

`ecctl configure --mode EcsRamRole` is not supported. `--mode` accepts `OAuth`
only, and an ecctl-native profile resolves only OAuth or a static credential.
Put this profile in the compatible `aliyun` configuration file.

## Configure with environment variables

```bash
export ALIBABA_CLOUD_ECS_METADATA=<role-name>
```

The environment path is consulted when no stored profile is selected, or when
`ALIBABA_CLOUD_IGNORE_PROFILE=TRUE` forces the environment-only path. In the
environment chain it is tested after an AccessKey pair and after a complete
OIDC set.

Two further variables control the metadata service itself:

| Variable | Effect |
|---|---|
| `ALIBABA_CLOUD_IMDSV1_DISABLED` | When true, refuse to fall back to IMDSv1 and require the hardened IMDSv2 path |
| `ALIBABA_CLOUD_ECS_METADATA_DISABLED` | When true, disable metadata credential acquisition entirely |

Both are read as booleans; `1` and `true` are accepted case-insensitively.

## Profile fields

| Field | Required | Notes |
|---|---|---|
| `mode` | No | `EcsRamRole`. Inferred when `ram_role_name` is present |
| `ram_role_name` | No | Falls back to `ALIBABA_CLOUD_ECS_METADATA` when empty |

`ram_role_name` may be left empty. The metadata service can report the attached
role, so an empty name still resolves. Supplying the name is stricter: it fails
when a different role is attached than the one the profile expects.

```json
{
  "name": "instance",
  "mode": "EcsRamRole",
  "ram_role_name": "my-ecs-role",
  "region_id": "cn-hangzhou"
}
```

## Verify

```bash
ecctl --profile instance configure get
ecctl --profile instance --region cn-hangzhou ecs region list
```

Run the check on the instance itself. From a workstation the command fails on
metadata acquisition rather than on credential validity, which is not a useful
signal.

## Renewal

Credentials from IMDS are renewable. `ecctl` keeps the provider for the whole
command and refreshes before a later signed request when the credential is
close to expiry, so long operations do not fail partway through.

The first renewable credential pins the canonical identity. A refreshed
credential for a different role is rejected before it can sign a request, so a
role change on the instance cannot silently switch accounts mid-command.

## Related

- [OIDC](./oidc.md) for Kubernetes and other OIDC workload identities
- [RAM role ARN](./ram-role-arn.md) for assuming a role from a source credential
- [Credentials overview](./index.md) for resolution order

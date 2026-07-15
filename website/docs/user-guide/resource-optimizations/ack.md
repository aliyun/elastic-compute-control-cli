---
title: ACK Optimizations
description: ACK cluster, node-pool, kubeconfig, permission, and version workflows in ecctl.
---

# ACK Optimizations

The resource IDs and response values below are illustrative. Replace them with
values from your account when running the commands.

## Clusters

### Route one update action to the required API

ACK exposes cluster settings, edition migration, tag replacement, tag addition,
and tag removal through different OpenAPI operations. ecctl keeps these changes
under `ack update` and selects the operation from the supplied fields.
Conflicting tag modes are rejected before execution.

For example, changing the cluster name uses `ModifyCluster` directly or
`--name` through ecctl:

Alibaba Cloud CLI:

```bash
aliyun cs ModifyCluster \
  --ClusterId c-bp1234567890example \
  --body '{"cluster_name":"prod"}'
```

ecctl:

```bash
ecctl ack update c-bp1234567890example \
  --region cn-beijing \
  --name prod
```

Edition migration uses a different OpenAPI, while the ecctl resource action
stays the same:

Alibaba Cloud CLI:

```bash
aliyun cs MigrateCluster \
  --cluster_id c-bp1234567890example \
  --body '{"cluster_spec":"ack.pro.small"}'
```

ecctl:

```bash
ecctl ack update c-bp1234567890example \
  --region cn-beijing \
  --to-edition ack.pro.small
```

### Merge optional cluster details on demand

Cluster details, cloud resources, tags, and policy-governance data come from
separate ACK APIs. A direct caller must invoke each API and merge the responses.
ecctl runs only the detail queries selected by `--with-resources`,
`--with-tags`, and `--with-policy-governance`.

Alibaba Cloud CLI:

```bash
aliyun cs DescribeClusterDetail \
  --ClusterId c-bp1234567890example
aliyun cs DescribeClusterResources \
  --ClusterId c-bp1234567890example
aliyun cs ListTagResources \
  --region_id cn-beijing \
  --resource_type CLUSTER \
  --resource_ids '["c-bp1234567890example"]'
```

ecctl:

```bash
ecctl ack get c-bp1234567890example \
  --region cn-beijing \
  --with-resources \
  --with-tags
```

The OpenAPI responses stay separate, while ecctl places the selected details
under one `cluster` object:

```json
// DescribeClusterDetail
{"cluster_id":"c-bp1234567890example","name":"prod",...}

// DescribeClusterResources
{"items":[{"resource_id":"i-bp1234567890example",...}],...}

// ListTagResources
{"tag_resources":{"tag_resource":[{"tag_key":"env","tag_value":"prod",...}]},...}

// ecctl
{
  "cluster": {
    "id": "c-bp1234567890example",
    "name": "prod",
    "resources": [{"resource_id": "i-bp1234567890example", ...}],
    "tags": [{"Key": "env", "Value": "prod"}],
    ...
  }
}
```

The ecctl command does not call `DescribePolicyGovernanceInCluster` because
`--with-policy-governance` was not supplied.

### Select the account or regional list API

ACK provides `DescribeClustersV1` for the default account list and
`DescribeClustersForRegion` for the regional cross-account mode. Direct callers
must select the operation. ecctl selects it from `--cross-account` while keeping
the same list command.

Alibaba Cloud CLI:

```bash
aliyun cs DescribeClustersV1 \
  --region_id cn-beijing \
  --page_number 1 \
  --page_size 20
```

ecctl:

```bash
ecctl ack list \
  --region cn-beijing \
  --page 1 \
  --limit 20
```

For regional cross-account listing, the equivalent pair is:

Alibaba Cloud CLI:

```bash
aliyun cs DescribeClustersForRegion \
  --region_id cn-beijing \
  --page_number 1 \
  --page_size 20
```

ecctl:

```bash
ecctl ack list \
  --region cn-beijing \
  --cross-account \
  --page 1 \
  --limit 20
```

See the [ACK cluster reference](../../reference/resources/ack/ack.md).

## Node pools

### Route node-pool changes by input

Node-pool configuration, desired size, node configuration, and tags use
different ACK operations. ecctl keeps them under `nodepool update` and runs only
the workflows selected by the supplied fields. `--with-node-config` explicitly
enables node-level configuration.

For example, scaling directly requires `ScaleClusterNodePool` and its request
body. ecctl exposes the desired size as a resource field:

Alibaba Cloud CLI:

```bash
aliyun cs ScaleClusterNodePool \
  --ClusterId c-bp1234567890example \
  --NodepoolId np-bp1234567890example \
  --body '{"desired_size":3}'
```

ecctl:

```bash
ecctl ack nodepool update np-bp1234567890example \
  --region cn-beijing \
  --cluster c-bp1234567890example \
  --desired-size 3
```

Changing node configuration routes to `ModifyNodePoolNodeConfig` only when the
mode and configuration are both present:

Alibaba Cloud CLI:

```bash
aliyun cs ModifyNodePoolNodeConfig \
  --ClusterId c-bp1234567890example \
  --NodepoolId np-bp1234567890example \
  --body '{"kubelet_config":{"registryPullQPS":10}}'
```

ecctl:

```bash
ecctl ack nodepool update np-bp1234567890example \
  --region cn-beijing \
  --cluster c-bp1234567890example \
  --with-node-config \
  --node-config @node-config.json
```

### Select node repair or vulnerability repair

ACK separates node repair and vulnerability repair into
`RepairClusterNodePool` and `FixNodePoolVuls`. ecctl selects the workflow from
`--node` or `--vulnerabilities`; the two modes cannot be combined.

Alibaba Cloud CLI:

```bash
aliyun cs FixNodePoolVuls \
  --cluster_id c-bp1234567890example \
  --nodepool_id np-bp1234567890example \
  --body '{"vuls":["CVE-2026-12345"]}'
```

ecctl:

```bash
ecctl ack nodepool repair np-bp1234567890example \
  --region cn-beijing \
  --cluster c-bp1234567890example \
  --vulnerabilities CVE-2026-12345
```

Supplying `--node node-bp1234567890example` instead routes the ecctl command to
`RepairClusterNodePool`.

### Attach instances or print the attach script

ACK uses `AttachInstancesToNodePool` to attach ECS instances and
`DescribeClusterAttachScripts` to generate an attach script. ecctl keeps both
outcomes under `nodepool attach`. `--print-script-only` returns the script and
does not attach an instance.

Alibaba Cloud CLI:

```bash
aliyun cs AttachInstancesToNodePool \
  --ClusterId c-bp1234567890example \
  --NodepoolId np-bp1234567890example \
  --body '{"instances":["i-bp1234567890example"]}'
```

ecctl:

```bash
ecctl ack nodepool attach np-bp1234567890example \
  --region cn-beijing \
  --cluster c-bp1234567890example \
  --instance i-bp1234567890example
```

To retrieve a script instead, use the other OpenAPI directly or change the
ecctl mode:

Alibaba Cloud CLI:

```bash
aliyun cs DescribeClusterAttachScripts \
  --ClusterId c-bp1234567890example
```

ecctl:

```bash
ecctl ack nodepool attach np-bp1234567890example \
  --region cn-beijing \
  --cluster c-bp1234567890example \
  --print-script-only
```

See the [node-pool reference](../../reference/resources/ack/nodepool.md).

## Kubeconfig

### Select owner or subaccount configuration

ACK has separate APIs for the current cluster owner and a RAM subaccount. A
direct caller must select the operation and unwrap `config` and `expiration`.
ecctl selects the subaccount path when `--user-id` is present and returns a
normalized `kubeconfig` object.

Alibaba Cloud CLI:

```bash
aliyun cs DescribeSubaccountK8sClusterUserConfig \
  --ClusterId c-bp1234567890example \
  --Uid 1234567890 \
  --TemporaryDurationMinutes 60
```

ecctl:

```bash
ecctl ack kubeconfig create \
  --cluster c-bp1234567890example \
  --user-id 1234567890 \
  --expire-time 60
```

```json
// OpenAPI
{"config": "apiVersion: v1\n...", "expiration": "2026-07-13T13:00:00Z", ...}

// ecctl
{
  "kubeconfig": {
    "cluster": "c-bp1234567890example",
    "user_id": "1234567890",
    "config": "apiVersion: v1\n...",
    "expiration": "2026-07-13T13:00:00Z"
  }
}
```

Omitting `--user-id` makes ecctl use `DescribeClusterUserKubeconfig` instead.

### Update expiry or revoke access

ACK uses one API to change subaccount kubeconfig expiry and another to revoke
the current cluster kubeconfig. ecctl exposes them as explicit resource actions.

Alibaba Cloud CLI:

```bash
aliyun cs UpdateK8sClusterUserConfigExpire \
  --ClusterId c-bp1234567890example \
  --body '{"expire_hour":24,"user":"1234567890"}'
```

ecctl:

```bash
ecctl ack kubeconfig update \
  --cluster c-bp1234567890example \
  --user-id 1234567890 \
  --expire-time 24
```

Revocation uses the corresponding pair:

Alibaba Cloud CLI:

```bash
aliyun cs RevokeK8sClusterKubeConfig \
  --ClusterId c-bp1234567890example
```

ecctl:

```bash
ecctl ack kubeconfig revoke \
  --cluster c-bp1234567890example
```

See the [kubeconfig reference](../../reference/resources/ack/kubeconfig.md).

## Permissions

### Incremental update or full replacement with read-back

ACK exposes incremental permission updates and full replacement through
different APIs. A direct caller must choose the operation and then call
`DescribeUserPermission` to read the effective state. ecctl selects the update
mode from `--replace` and performs the read-back automatically.

Alibaba Cloud CLI:

```bash
aliyun cs UpdateUserPermissions \
  --uid 1234567890 \
  --mode patch \
  --body '[{"cluster":"c-bp1234567890example","role_type":"cluster","role_name":"dev"}]'
aliyun cs DescribeUserPermission \
  --uid 1234567890
```

ecctl:

```bash
ecctl ack permission update \
  --user-id 1234567890 \
  --permission cluster=c-bp1234567890example,role-type=cluster,role-name=dev
```

The direct calls return the update acknowledgement and read-back separately.
ecctl returns the effective permissions from its automatic read-back together
with the actions it performed:

```json
// UpdateUserPermissions
{"RequestId":"req-update",...}

// DescribeUserPermission
{"body":[{"resource_id":"c-bp1234567890example","role_type":"cluster","role_name":"dev",...}],...}

// ecctl
{
  "actions": [
    {"action_name": "UpdateUserPermissions", ...},
    {"action_name": "DescribeUserPermission", ...}
  ],
  "permission": {
    "user_id": "1234567890",
    "permissions": [
      {"resource_id": "c-bp1234567890example", "role_type": "cluster", "role_name": "dev", ...}
    ]
  }
}
```

For full replacement, a direct caller uses `GrantPermissions`; ecctl adds
`--replace` to the same resource action.

Alibaba Cloud CLI:

```bash
aliyun cs GrantPermissions \
  --uid 1234567890 \
  --body '[{"cluster":"c-bp1234567890example","role_type":"cluster","role_name":"dev"}]'
```

ecctl:

```bash
ecctl ack permission update \
  --user-id 1234567890 \
  --permission cluster=c-bp1234567890example,role-type=cluster,role-name=dev \
  --replace
```

### Cluster-scoped or user-scoped cleanup

ACK separates cleanup for one cluster from cleanup across a user's clusters. A
direct caller must select `CleanClusterUserPermissions` or
`CleanUserPermissions` and query the remaining permissions. ecctl requires
`--cluster` or the explicit `--all-clusters` mode, then performs the read-back.

Alibaba Cloud CLI:

```bash
aliyun cs CleanClusterUserPermissions \
  --Uid 1234567890 \
  --ClusterId c-bp1234567890example
aliyun cs DescribeUserPermission \
  --uid 1234567890
```

ecctl:

```bash
ecctl ack permission delete \
  --user-id 1234567890 \
  --cluster c-bp1234567890example
```

To clean all clusters, use `CleanUserPermissions` directly or replace
`--cluster ...` with `--all-clusters` in the ecctl command.

See the [permission reference](../../reference/resources/ack/permission.md).

## Versions

### Validate and map the metadata selector

`DescribeKubernetesVersionMetadata` requires a region and cluster type. ecctl
accepts the cluster type as `--cluster-type` or through
`--filter cluster-type=...` and validates the selector before calling ACK.

Alibaba Cloud CLI:

```bash
aliyun cs DescribeKubernetesVersionMetadata \
  --Region cn-beijing \
  --ClusterType ManagedKubernetes \
  --runtime containerd
```

ecctl:

```bash
ecctl ack version list \
  --region cn-beijing \
  --cluster-type ManagedKubernetes \
  --runtime containerd
```

Omitting the cluster type from the ecctl command is rejected before an OpenAPI
request is sent.

See the [version reference](../../reference/resources/ack/version.md).

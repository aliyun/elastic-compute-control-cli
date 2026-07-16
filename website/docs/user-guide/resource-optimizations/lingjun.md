---
title: Lingjun Optimizations
description: Lingjun cluster and VPD workflows in the public ecctl surface.
---

# Lingjun Optimizations

The optimizations below apply to Lingjun clusters and VPD network segments.
The resource IDs and response values are illustrative.

## Clusters

### Scale workflow routing

The Lingjun OpenAPI exposes `ExtendCluster` and `ShrinkCluster` as separate
operations. ecctl uses one `cluster update` action and selects the operation
from `--extend` or `--shrink`. Supplying both modes is rejected before a request
is sent.

For example, both commands below extend the same node group by one node:

Alibaba Cloud CLI:

```bash
aliyun eflo-controller ExtendCluster \
  --ClusterId c-bp1234567890example \
  --NodeGroups '[{"NodeGroupId":"ng-bp1234567890example","NodeCount":1}]'
```

ecctl:

```bash
ecctl lingjun cluster update c-bp1234567890example \
  --region cn-beijing \
  --extend '[{"NodeGroupId":"ng-bp1234567890example","NodeCount":1}]'
```

To shrink the cluster, a direct caller switches to `ShrinkCluster`, while the
ecctl command keeps the same resource action and changes only the input mode:

Alibaba Cloud CLI:

```bash
aliyun eflo-controller ShrinkCluster \
  --ClusterId c-bp1234567890example \
  --NodeGroups '[{"NodeGroupId":"ng-bp1234567890example","Nodes":[{"NodeId":"node-bp1234567890example"}]}]'
```

ecctl:

```bash
ecctl lingjun cluster update c-bp1234567890example \
  --region cn-beijing \
  --shrink '[{"NodeGroupId":"ng-bp1234567890example","Nodes":[{"NodeId":"node-bp1234567890example"}]}]'
```

### Optional node details

A direct caller first invokes `DescribeCluster`, then separately queries
standard nodes or hyper nodes and merges the responses. ecctl runs only the
detail queries selected by `--with-nodes` or `--with-hyper-nodes` and returns
one resource view.

Alibaba Cloud CLI:

```bash
aliyun eflo-controller DescribeCluster \
  --ClusterId c-bp1234567890example
aliyun eflo-controller ListClusterNodes \
  --ClusterId c-bp1234567890example
```

ecctl:

```bash
ecctl lingjun cluster get c-bp1234567890example \
  --region cn-beijing \
  --with-nodes
```

OpenAPI returns cluster attributes and nodes separately. ecctl normalizes and
merges the requested nodes into the cluster object:

```json
// DescribeCluster
{"ClusterId":"c-bp1234567890example","ClusterName":"train","OperatingState":"running",...}

// ListClusterNodes
{"Nodes":[{"NodeId":"node-bp1234567890example","Hostname":"worker-1","Status":"running",...}],...}

// ecctl
{
  "cluster": {
    "id": "c-bp1234567890example",
    "name": "train",
    "status": "running",
    "nodes": [
      {"id": "node-bp1234567890example", "hostname": "worker-1", "status": "running", ...}
    ],
    ...
  }
}
```

This ecctl example does not call `ListClusterHyperNodes` because
`--with-hyper-nodes` was not supplied.

See the [Lingjun cluster reference](../../reference/resources/lingjun/cluster.md).

## VPDs

### Wait for asynchronous creation

`CreateVpd` returns before the VPD is ready. A direct caller must retain the VPD
ID and poll `GetVpd` or `ListVpds` until the state becomes `Available`. ecctl
does this by default and returns the final resource view. `--no-wait` keeps the
asynchronous behavior when required.

Alibaba Cloud CLI:

```bash
aliyun eflo CreateVpd \
  --RegionId cn-wulanchabu \
  --VpdName train-vpd \
  --Cidr 10.0.0.0/16
aliyun eflo GetVpd \
  --RegionId cn-wulanchabu \
  --VpdId vpd-bp1234567890example
```

ecctl:

```bash
ecctl lingjun vpd create \
  --region cn-wulanchabu \
  --name train-vpd \
  --cidr 10.0.0.0/16
```

The raw create response contains the new ID and an intermediate state. The
default ecctl result contains the VPD after it reaches `Available`:

```json
// CreateVpd
{"Content": {"VpdId": "vpd-bp1234567890example", ...}, ...}

// ecctl
{"vpd": {"id": "vpd-bp1234567890example", "status": "Available", ...}, ...}
```

### Optional routes and grants

VPD attributes, route entries, and grant rules come from separate OpenAPI
operations. ecctl merges only the related data explicitly requested by the
caller.

Alibaba Cloud CLI:

```bash
aliyun eflo GetVpd \
  --RegionId cn-wulanchabu \
  --VpdId vpd-bp1234567890example
aliyun eflo ListVpdRouteEntries \
  --RegionId cn-wulanchabu \
  --VpdId vpd-bp1234567890example
```

ecctl:

```bash
ecctl lingjun vpd get vpd-bp1234567890example \
  --region cn-wulanchabu \
  --with-routes
```

The route list remains a separate OpenAPI response. ecctl normalizes it under
the VPD object:

```json
// GetVpd
{"Content":{"VpdId":"vpd-bp1234567890example","VpdName":"train-vpd","Status":"Available",...},...}

// ListVpdRouteEntries
{"Content":{"Data":[{"VpdRouteEntryId":"rte-bp1234567890example","DestinationCidrBlock":"0.0.0.0/0",...}]},...}

// ecctl
{
  "vpd": {
    "id": "vpd-bp1234567890example",
    "name": "train-vpd",
    "status": "Available",
    "routes": [
      {"id": "rte-bp1234567890example", "destination_cidr": "0.0.0.0/0", ...}
    ],
    ...
  }
}
```

This ecctl command does not call `ListVpdGrantRules`; add `--with-grants` when
grant rules are also needed.

### Secondary CIDR changes

The primary VPD CIDR is immutable. OpenAPI exposes separate operations for
associating and unassociating secondary CIDRs. ecctl represents both changes on
one resource update with a `+` or `-` prefix.

Alibaba Cloud CLI:

```bash
aliyun eflo AssociateVpdCidrBlock \
  --RegionId cn-wulanchabu \
  --VpdId vpd-bp1234567890example \
  --SecondaryCidrBlock 172.16.0.0/16
```

ecctl:

```bash
ecctl lingjun vpd update vpd-bp1234567890example \
  --region cn-wulanchabu \
  --cidr +172.16.0.0/16
```

Removing the same secondary CIDR uses `UnAssociateVpdCidrBlock` directly or
`--cidr -172.16.0.0/16` through ecctl.

See the [Lingjun VPD reference](../../reference/resources/lingjun/vpd.md).

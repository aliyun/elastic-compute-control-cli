---
title: ECS Optimizations
description: ECS input conversions, multi-API workflows, and output normalization in ecctl.
---

# ECS Optimizations

ECS resource commands replace API-oriented calls with resource actions. The
sections below describe behavior specific to each ECS resource, followed by
concrete Alibaba Cloud CLI and ecctl examples. Common output, pagination, error,
and schema contracts are described in [Common Differences](../common-differences.md).

The resource IDs and response values below are illustrative. Replace them with
values from your account when running the commands.

## Instances

### Resolve an image name before creation

`RunInstances.ImageId` requires an image ID. A direct caller that starts from an
image name must query `DescribeImages`, select a result, and copy its ID into
`RunInstances`. ecctl treats a non-empty `--image` value that does not end in
`.vhd` as a name, performs the lookup, and passes the resolved ID to
`RunInstances`. A value ending in `.vhd` is passed through as an ID.

Alibaba Cloud CLI:

```bash
aliyun ecs DescribeImages \
  --RegionId cn-hangzhou \
  --ImageName '*aliyun*' \
  --InstanceType ecs.g6.large
```

If that query returns
`ImageId=aliyun_4_x64_20G_agentic_alibase_20260704.vhd`, the direct create call
must use the returned value:

Alibaba Cloud CLI:

```bash
aliyun ecs RunInstances \
  --RegionId cn-hangzhou \
  --InstanceType ecs.g6.large \
  --ImageId aliyun_4_x64_20G_agentic_alibase_20260704.vhd \
  --SecurityGroupId sg-bp1234567890example \
  --VSwitchId vsw-bp1234567890example
```

ecctl:

```bash
ecctl ecs instance create \
  --region cn-hangzhou \
  --type ecs.g6.large \
  --image aliyun \
  --sg sg-bp1234567890example \
  --vswitch vsw-bp1234567890example
```

The resolved ID can vary by region, instance type, and time.

### Route fields to their owning update APIs

ECS instance attributes, specifications, RAM roles, security groups, and tags
are owned by different APIs. A direct caller must select the operation for each
change. `ecctl ecs instance update` selects the operation from the modeled
fields; `--api-param` remains a per-API escape hatch where supported.

Suppose one maintenance change must rename an instance to `web-01` and change
its type to `ecs.g6.large`. Alibaba Cloud CLI requires two commands because the
fields belong to `ModifyInstanceAttribute` and `ModifyInstanceSpec`. ecctl
accepts both fields in one resource update and routes each field to its owning
API. The type change uses the default postpaid path in this example.

Alibaba Cloud CLI:

```bash
aliyun ecs ModifyInstanceAttribute \
  --InstanceId i-bp1234567890example \
  --InstanceName web-01
aliyun ecs ModifyInstanceSpec \
  --InstanceId i-bp1234567890example \
  --InstanceType ecs.g6.large
```

ecctl:

```bash
ecctl ecs instance update i-bp1234567890example \
  --region cn-hangzhou \
  --name web-01 \
  --type ecs.g6.large
```

### Reconcile the desired security-group set

OpenAPI exposes `JoinSecurityGroup` and `LeaveSecurityGroup` as individual
changes. A direct caller must read the current instance, calculate the set
difference, and issue each join or leave request. ecctl treats
`--security-group-ids` as the desired final set and performs that reconciliation.

If the instance currently belongs to `sg-a` and `sg-b`, changing the desired set
to `sg-b,sg-c` requires these direct calls:

Alibaba Cloud CLI:

```bash
aliyun ecs DescribeInstances \
  --RegionId cn-hangzhou \
  --InstanceIds '["i-bp1234567890example"]'
aliyun ecs LeaveSecurityGroup \
  --InstanceId i-bp1234567890example \
  --SecurityGroupId sg-a
aliyun ecs JoinSecurityGroup \
  --InstanceId i-bp1234567890example \
  --SecurityGroupId sg-c
```

ecctl:

```bash
ecctl ecs instance update i-bp1234567890example \
  --region cn-hangzhou \
  --security-group-ids sg-b,sg-c
```

### Select single-instance or batch actions

ECS has separate APIs for single-instance and batch start, stop, reboot, and
delete operations. ecctl accepts one or several IDs on the same resource action
and selects the API from the number of IDs.

Alibaba Cloud CLI:

```bash
aliyun ecs StartInstances \
  --RegionId cn-hangzhou \
  --InstanceId.1 i-bp1234567890example \
  --InstanceId.2 i-bp0987654321example
```

ecctl:

```bash
ecctl ecs instance start \
  i-bp1234567890example \
  i-bp0987654321example \
  --region cn-hangzhou
```

With one ID, the same ecctl action calls `StartInstance`; with two IDs, as
shown above, it calls `StartInstances`.

### Merge optional instance details on demand

Auto-renew settings, maintenance attributes, RAM roles, user data, VNC URLs,
Cloud Assistant state, plugin state, and tags come from separate APIs. ecctl
runs only the detail queries selected with `--with-*` and merges them into one
instance view.

Alibaba Cloud CLI:

```bash
aliyun ecs DescribeInstances \
  --RegionId cn-hangzhou \
  --InstanceIds '["i-bp1234567890example"]'
aliyun ecs DescribeInstanceRamRole \
  --RegionId cn-hangzhou \
  --InstanceIds '["i-bp1234567890example"]'
```

ecctl:

```bash
ecctl ecs instance get i-bp1234567890example \
  --region cn-hangzhou \
  --with-ram-role
```

Alibaba Cloud CLI returns two independent responses:

```json
// DescribeInstances
{
  "Instances": {
    "Instance": [{"InstanceId": "i-bp1234567890example", "InstanceName": "web-01", ...}]
  },
  ...
}

// DescribeInstanceRamRole
{
  "InstanceRamRoleSets": {
    "InstanceRamRoleSet": [{"InstanceId": "i-bp1234567890example", "RamRoleName": "ecs-role"}]
  },
  ...
}
```

ecctl merges the selected detail into the instance view:

```json
{
  "instance": {
    "id": "i-bp1234567890example",
    "name": "web-01",
    "ram_role": {
      "InstanceRamRoleSets": {
        "InstanceRamRoleSet": [{"InstanceId": "i-bp1234567890example", "RamRoleName": "ecs-role"}]
      },
      ...
    },
    ...
  }
}
```

The ecctl command does not run the other optional detail APIs because their
flags were not supplied.

### Decode Cloud Assistant output

Cloud Assistant returns command output as Base64. Direct callers must decode
`DescribeInvocationResults.Output` themselves. `instance exec` preserves the
raw `output` and adds `output_text` when decoding succeeds; invalid Base64 is
not exposed as decoded text.

Alibaba Cloud CLI:

```bash
aliyun ecs RunCommand \
  --RegionId cn-hangzhou \
  --InstanceId.1 i-bp1234567890example \
  --Type RunShellScript \
  --CommandContent 'printf uptime'
aliyun ecs DescribeInvocationResults \
  --RegionId cn-hangzhou \
  --InvokeId t-bp1234567890example
```

ecctl:

```bash
ecctl ecs instance exec i-bp1234567890example \
  --region cn-hangzhou \
  --command 'printf uptime' \
  --command-type RunShellScript
```

```json
// OpenAPI
{"Output": "dXB0aW1l", ...}

// ecctl
{"output": "dXB0aW1l", "output_text": "uptime", ...}
```

### Add recovery guidance to stock and state errors

ECS and Alibaba Cloud CLI return the provider error code, message, and Request
ID. ecctl preserves those fields in `actions` and adds a recovery command for
stock errors or an explanation of whether deletion requires a stop or force
release.

Alibaba Cloud CLI:

```bash
aliyun ecs RunInstances \
  --RegionId cn-shanghai \
  --ZoneId cn-shanghai-g \
  --InstanceType ecs.g6.large \
  --ImageId aliyun_3_x64_20G_alibase_20240528.vhd \
  --SecurityGroupId sg-bp1234567890example \
  --VSwitchId vsw-bp1234567890example
```

ecctl:

```bash
ecctl ecs instance create \
  --region cn-shanghai \
  --zone cn-shanghai-g \
  --type ecs.g6.large \
  --image aliyun_3_x64_20G_alibase_20240528.vhd \
  --sg sg-bp1234567890example \
  --vswitch vsw-bp1234567890example
```

For an unavailable instance type, the critical difference is:

```json
// OpenAPI
{"Code": "InvalidResourceType.NotSupported", "Message": "...", "RequestId": "..."}

// ecctl
{
  "error": {
    "field": "type",
    "suggested_action": "ecctl call ecs DescribeAvailableResource --region cn-shanghai --ZoneId cn-shanghai-g --DestinationResource InstanceType --InstanceType ecs.g6.large",
    ...
  },
  "actions": [{"code": "InvalidResourceType.NotSupported", "request_id": "...", ...}]
}
```

See the [instance reference](../../reference/resources/ecs/instance.md).

## Security groups

### Expand security-group rule shorthand

ECS authorization APIs require separate protocol, port-range, and CIDR fields.
ecctl accepts `protocol:port@cidr`, `protocol:port:cidr`, and
`direction:protocol:port:cidr`, then expands the value before the API call.
Malformed structures are rejected locally; the service still validates values
such as the CIDR.

Alibaba Cloud CLI:

```bash
aliyun ecs AuthorizeSecurityGroup \
  --RegionId cn-hangzhou \
  --SecurityGroupId sg-bp1234567890example \
  --IpProtocol tcp \
  --PortRange 80/80 \
  --SourceCidrIp 0.0.0.0/0
```

ecctl:

```bash
ecctl ecs sg authorize sg-bp1234567890example \
  --region cn-hangzhou \
  --rule tcp:80@0.0.0.0/0
```

### Normalize protocol, policy, and port values

Direct callers must construct ECS values such as `IpProtocol=tcp`,
`Policy=accept`, and `PortRange=100/200`. ecctl lowercases protocol and policy
values and converts compact port forms to ECS ranges. ICMP `-1` remains
`-1/-1`.

Alibaba Cloud CLI:

```bash
aliyun ecs AuthorizeSecurityGroup \
  --RegionId cn-hangzhou \
  --SecurityGroupId sg-bp1234567890example \
  --IpProtocol tcp \
  --PortRange 100/200 \
  --Policy accept \
  --SourceCidrIp 10.0.0.0/8
```

ecctl:

```bash
ecctl ecs sg authorize sg-bp1234567890example \
  --region cn-hangzhou \
  --rule TCP:100-200@10.0.0.0/8
```

### Route ingress and egress operations

ECS uses separate authorize, revoke, and modify APIs for ingress and egress.
ecctl keeps the direction as a resource-rule field and selects the operation.
Ingress uses `SourceCidrIp`; egress uses `DestCidrIp`.

Alibaba Cloud CLI:

```bash
aliyun ecs AuthorizeSecurityGroupEgress \
  --RegionId cn-hangzhou \
  --SecurityGroupId sg-bp1234567890example \
  --IpProtocol tcp \
  --PortRange 443/443 \
  --DestCidrIp 0.0.0.0/0
```

ecctl:

```bash
ecctl ecs sg authorize sg-bp1234567890example \
  --region cn-hangzhou \
  --direction egress \
  --rule tcp:443@0.0.0.0/0
```

### Merge security-group references on demand

Security-group attributes and reference relationships come from different APIs.
A direct caller must query both and merge them. ecctl calls
`DescribeSecurityGroupReferences` only when `--with-references` is present.

Alibaba Cloud CLI:

```bash
aliyun ecs DescribeSecurityGroupAttribute \
  --RegionId cn-hangzhou \
  --SecurityGroupId sg-bp1234567890example
aliyun ecs DescribeSecurityGroupReferences \
  --RegionId cn-hangzhou \
  --SecurityGroupId.1 sg-bp1234567890example
```

ecctl:

```bash
ecctl ecs sg get sg-bp1234567890example \
  --region cn-hangzhou \
  --with-references
```

The two OpenAPI responses remain separate. ecctl merges the references into the
security-group view:

```json
// DescribeSecurityGroupAttribute
{"SecurityGroupId":"sg-bp1234567890example","SecurityGroupName":"web",...}

// DescribeSecurityGroupReferences
{"SecurityGroupReferences":{"SecurityGroupReference":[{"SecurityGroupId":"sg-bp1234567890example",...}]},...}

// ecctl
{
  "security_group": {
    "id": "sg-bp1234567890example",
    "name": "web",
    "references": [{"SecurityGroupId": "sg-bp1234567890example", ...}],
    ...
  }
}
```

See the [security-group reference](../../reference/resources/ecs/sg.md).

## Disks

### Route disk fields to the owning API

Disk attributes, size, performance, billing, deployment, and default encryption
use different ECS APIs. A direct caller must select the operation. `disk update`
runs only the workflow selected by the supplied fields.

Alibaba Cloud CLI:

```bash
aliyun ecs ResizeDisk \
  --DiskId d-bp1234567890example \
  --NewSize 200 \
  --Type online
```

ecctl:

```bash
ecctl ecs disk update d-bp1234567890example \
  --region cn-hangzhou \
  --size 200 \
  --resize-type online
```

Here ecctl routes the size change to `ResizeDisk`; it does not call the other
disk update APIs.

See the [disk reference](../../reference/resources/ecs/disk.md).

## Snapshots

### Route snapshot changes to the owning API

Snapshot attributes, categories, locks, and snapshot-service activation use
different operations. ecctl keeps them under `snapshot update` and selects the
operation from the requested field.

Alibaba Cloud CLI:

```bash
aliyun ecs ModifySnapshotAttribute \
  --SnapshotId s-bp1234567890example \
  --SnapshotName nightly
```

ecctl:

```bash
ecctl ecs snapshot update s-bp1234567890example \
  --region cn-hangzhou \
  --name nightly
```

A category change instead routes to `ModifySnapshotCategory`, while a lock or
unlock input selects `LockSnapshot` or `UnlockSnapshot`.

### Merge optional snapshot details

Lock state, snapshot-chain data, monitor data, package information, and usage
come from separate APIs. ecctl adds only the details selected with `--with-*`.

Alibaba Cloud CLI:

```bash
aliyun ecs DescribeSnapshots \
  --RegionId cn-hangzhou \
  --SnapshotIds '["s-bp1234567890example"]'
aliyun ecs DescribeLockedSnapshots \
  --RegionId cn-hangzhou \
  --SnapshotIds.1 s-bp1234567890example
```

ecctl:

```bash
ecctl ecs snapshot get s-bp1234567890example \
  --region cn-hangzhou \
  --with-lock
```

The lock query remains a separate OpenAPI response, while ecctl adds it to the
snapshot object:

```json
// DescribeSnapshots
{"Snapshots":{"Snapshot":[{"SnapshotId":"s-bp1234567890example","SnapshotName":"nightly",...}]},...}

// DescribeLockedSnapshots
{"LockedSnapshots":{"LockedSnapshot":[{"SnapshotId":"s-bp1234567890example",...}]},...}

// ecctl
{
  "snapshot": {
    "id": "s-bp1234567890example",
    "name": "nightly",
    "locked_snapshots": [{"SnapshotId": "s-bp1234567890example", ...}],
    ...
  }
}
```

Adding `--with-usage` makes ecctl run the usage query as well; omitting it
avoids that call.

See the [snapshot reference](../../reference/resources/ecs/snapshot.md).

## Images

### Route attribute and sharing changes

Image attributes and share permissions use different ECS APIs. ecctl selects
the operation from the update field.

Alibaba Cloud CLI:

```bash
aliyun ecs ModifyImageAttribute \
  --RegionId cn-hangzhou \
  --ImageId m-bp1234567890example \
  --ImageName web-v2
```

ecctl:

```bash
ecctl ecs image update m-bp1234567890example \
  --region cn-hangzhou \
  --name web-v2
```

Supplying a share-permission input instead routes the ecctl command to
`ModifyImageSharePermission`.

### Merge optional image details

Share permissions and supported instance types come from separate queries.
ecctl invokes them only when `--with-share-permission` or
`--with-supported-instance-types` is requested.

Alibaba Cloud CLI:

```bash
aliyun ecs DescribeImages \
  --RegionId cn-hangzhou \
  --ImageId m-bp1234567890example
aliyun ecs DescribeImageSupportInstanceTypes \
  --RegionId cn-hangzhou \
  --ImageId m-bp1234567890example
```

ecctl:

```bash
ecctl ecs image get m-bp1234567890example \
  --region cn-hangzhou \
  --with-supported-instance-types
```

The supported types arrive in a separate OpenAPI response. ecctl puts the
normalized type list on the image object:

```json
// DescribeImages
{"Images":{"Image":[{"ImageId":"m-bp1234567890example","ImageName":"web-v2",...}]},...}

// DescribeImageSupportInstanceTypes
{"InstanceTypes":{"InstanceType":["ecs.g6.large",...]},...}

// ecctl
{
  "image": {
    "id": "m-bp1234567890example",
    "name": "web-v2",
    "supported_instance_types": ["ecs.g6.large", ...],
    ...
  }
}
```

See the [image reference](../../reference/resources/ecs/image.md).

## Auto snapshot policies

### Combine policy and disk-association changes

ECS uses separate APIs to modify an auto snapshot policy, apply it to disks,
and cancel disk associations. One ecctl `update` action can perform the selected
changes, and each field triggers only its corresponding operation.

Alibaba Cloud CLI:

```bash
aliyun ecs ModifyAutoSnapshotPolicyEx \
  --regionId cn-hangzhou \
  --autoSnapshotPolicyId sp-bp1234567890example \
  --autoSnapshotPolicyName daily
aliyun ecs ApplyAutoSnapshotPolicy \
  --regionId cn-hangzhou \
  --autoSnapshotPolicyId sp-bp1234567890example \
  --diskIds '["d-bp1234567890example"]'
aliyun ecs CancelAutoSnapshotPolicy \
  --regionId cn-hangzhou \
  --autoSnapshotPolicyId sp-bp1234567890example \
  --diskIds '["d-bp0987654321example"]'
```

ecctl:

```bash
ecctl ecs auto-snapshot-policy update sp-bp1234567890example \
  --region cn-hangzhou \
  --name daily \
  --attach-disk-id d-bp1234567890example \
  --detach-disk-id d-bp0987654321example
```

See the [auto snapshot policy reference](../../reference/resources/ecs/auto-snapshot-policy.md).

## Elastic network interfaces

### Route ENI changes to address and QoS APIs

ENI attributes, private IPv4 addresses, IPv6 addresses, prefixes, and QoS use
different operations. ecctl keeps them under `eni update` and runs only the
changes supplied by the caller.

Alibaba Cloud CLI:

```bash
aliyun ecs AssignPrivateIpAddresses \
  --RegionId cn-hangzhou \
  --NetworkInterfaceId eni-bp1234567890example \
  --PrivateIpAddress.1 10.0.0.8
```

ecctl:

```bash
ecctl ecs eni update eni-bp1234567890example \
  --region cn-hangzhou \
  --private-ip +10.0.0.8
```

Using a `-` prefix routes an address removal to
`UnassignPrivateIpAddresses`; attribute or QoS fields select their respective
APIs.

See the [ENI reference](../../reference/resources/ecs/eni.md).

## Prefix lists

### Express entry additions and removals in one update

`ModifyPrefixList` accepts separate add and remove entry sets. ecctl represents
both changes with repeated `--entry` values prefixed by `+` or `-`.

Alibaba Cloud CLI:

```bash
aliyun ecs ModifyPrefixList \
  --RegionId cn-hangzhou \
  --PrefixListId pl-bp1234567890example \
  --AddEntry.1.Cidr 10.0.1.0/24 \
  --AddEntry.1.Description app \
  --RemoveEntry.1.Cidr 10.0.0.0/24
```

ecctl:

```bash
ecctl ecs prefix-list update pl-bp1234567890example \
  --region cn-hangzhou \
  --entry +cidr=10.0.1.0/24,description=app \
  --entry -cidr=10.0.0.0/24
```

### Merge prefix-list associations

Prefix-list entries and the resources that reference the list come from
different queries. ecctl adds association data only when
`--with-associations` is supplied.

Alibaba Cloud CLI:

```bash
aliyun ecs DescribePrefixListAttributes \
  --RegionId cn-hangzhou \
  --PrefixListId pl-bp1234567890example
aliyun ecs DescribePrefixListAssociations \
  --RegionId cn-hangzhou \
  --PrefixListId pl-bp1234567890example
```

ecctl:

```bash
ecctl ecs prefix-list get pl-bp1234567890example \
  --region cn-hangzhou \
  --with-associations
```

OpenAPI returns the entries and associations in different response wrappers.
ecctl normalizes both under the prefix-list object:

```json
// DescribePrefixListAttributes
{"PrefixListId":"pl-bp1234567890example","Entries":{"Entry":[{"Cidr":"10.0.0.0/24",...}]},...}

// DescribePrefixListAssociations
{"PrefixListAssociations":{"PrefixListAssociation":[{"ResourceId":"sg-bp1234567890example","ResourceType":"securitygroup"}]},...}

// ecctl
{
  "prefix_list": {
    "id": "pl-bp1234567890example",
    "entries": [{"cidr": "10.0.0.0/24", ...}],
    "associations": [
      {"resource_id": "sg-bp1234567890example", "resource_type": "securitygroup"}
    ],
    ...
  }
}
```

See the [prefix-list reference](../../reference/resources/ecs/prefix-list.md).

## Port range lists

### Merge entries and associations on demand

Port-range-list entries and referencing resources use separate ECS APIs. A
direct caller must query and combine them. ecctl merges only the details
selected by `--with-entries` and `--with-associations`.

Alibaba Cloud CLI:

```bash
aliyun ecs DescribePortRangeListEntries \
  --RegionId cn-hangzhou \
  --PortRangeListId prl-bp1234567890example
aliyun ecs DescribePortRangeListAssociations \
  --RegionId cn-hangzhou \
  --PortRangeListId prl-bp1234567890example
```

ecctl:

```bash
ecctl ecs port-range-list get prl-bp1234567890example \
  --region cn-hangzhou \
  --with-entries \
  --with-associations
```

OpenAPI returns entries and associations separately. ecctl normalizes and
merges both collections into the port-range-list object:

```json
// DescribePortRangeListEntries
{"Entries":[{"PortRange":"80/80","Description":"http"}],...}

// DescribePortRangeListAssociations
{"PortRangeListAssociations":[{"ResourceId":"sg-bp1234567890example","ResourceType":"securitygroup"}],...}

// ecctl
{
  "port_range_list": {
    "id": "prl-bp1234567890example",
    "entries": [{"port_range": "80/80", "description": "http"}],
    "associations": [
      {"resource_id": "sg-bp1234567890example", "resource_type": "securitygroup"}
    ],
    ...
  }
}
```

See the [port range list reference](../../reference/resources/ecs/port-range-list.md).

## Launch templates

### Create a version or switch the default version

ECS uses `CreateLaunchTemplateVersion` to create a version and
`ModifyLaunchTemplateDefaultVersion` to change the default. ecctl keeps both
modes under `launch-template update`; incompatible modes are rejected by the
command contract.

Alibaba Cloud CLI:

```bash
aliyun ecs CreateLaunchTemplateVersion \
  --RegionId cn-hangzhou \
  --LaunchTemplateId lt-bp1234567890example \
  --ImageId aliyun_3_x64_20G_alibase_20240528.vhd
```

ecctl:

```bash
ecctl ecs launch-template update lt-bp1234567890example \
  --region cn-hangzhou \
  --create-version \
  --image aliyun_3_x64_20G_alibase_20240528.vhd
```

To switch the default to version 2, use the other OpenAPI operation:

Alibaba Cloud CLI:

```bash
aliyun ecs ModifyLaunchTemplateDefaultVersion \
  --RegionId cn-hangzhou \
  --LaunchTemplateId lt-bp1234567890example \
  --DefaultVersionNumber 2
```

ecctl:

```bash
ecctl ecs launch-template update lt-bp1234567890example \
  --region cn-hangzhou \
  --default-version 2
```

### Delete one version or the whole template

ECS exposes separate APIs for version deletion and template deletion. ecctl
uses `--version` to distinguish the two outcomes.

Alibaba Cloud CLI:

```bash
aliyun ecs DeleteLaunchTemplateVersion \
  --RegionId cn-hangzhou \
  --LaunchTemplateId lt-bp1234567890example \
  --DeleteVersion.1 2
```

ecctl:

```bash
ecctl ecs launch-template delete lt-bp1234567890example \
  --region cn-hangzhou \
  --version 2
```

Omitting `--version` makes ecctl call `DeleteLaunchTemplate` for the whole
template.

### Merge template versions on demand

Template metadata and its version list come from separate queries. ecctl calls
`DescribeLaunchTemplateVersions` only when `--with-versions` is present.

Alibaba Cloud CLI:

```bash
aliyun ecs DescribeLaunchTemplates \
  --RegionId cn-hangzhou \
  --LaunchTemplateId.1 lt-bp1234567890example
aliyun ecs DescribeLaunchTemplateVersions \
  --RegionId cn-hangzhou \
  --LaunchTemplateId lt-bp1234567890example
```

ecctl:

```bash
ecctl ecs launch-template get lt-bp1234567890example \
  --region cn-hangzhou \
  --with-versions
```

OpenAPI returns template metadata and versions in separate wrappers. ecctl
merges normalized versions into the launch-template object:

```json
// DescribeLaunchTemplates
{"LaunchTemplateSets":{"LaunchTemplateSet":[{"LaunchTemplateId":"lt-bp1234567890example","DefaultVersionNumber":1,...}]},...}

// DescribeLaunchTemplateVersions
{"LaunchTemplateVersionSets":{"LaunchTemplateVersionSet":[{"LaunchTemplateId":"lt-bp1234567890example","VersionNumber":1,"DefaultVersion":true,...}]},...}

// ecctl
{
  "launch_template": {
    "id": "lt-bp1234567890example",
    "default_version": 1,
    "versions": [{"version": 1, "default": true, ...}],
    ...
  }
}
```

See the [launch-template reference](../../reference/resources/ecs/launch-template.md).

## Key pairs

### Select creation or public-key import

ECS separates generated key-pair creation from public-key import. ecctl selects
`ImportKeyPair` when `--public-key` is present and `CreateKeyPair` when it is
omitted.

Alibaba Cloud CLI:

```bash
aliyun ecs ImportKeyPair \
  --RegionId cn-hangzhou \
  --KeyPairName imported-key \
  --PublicKeyBody 'ssh-rsa AAAA...'
```

ecctl:

```bash
ecctl ecs keypair create \
  --region cn-hangzhou \
  --name imported-key \
  --public-key 'ssh-rsa AAAA...'
```

Without `--public-key`, the ecctl action generates a new key pair through
`CreateKeyPair`.

See the [key-pair reference](../../reference/resources/ecs/keypair.md).

## Cloud Assistant commands

### Merge invocation results on demand

Invocation metadata and its per-instance results come from separate ECS APIs. A
direct caller must query and merge them. ecctl runs the result query only when
`--with-results` is requested.

Alibaba Cloud CLI:

```bash
aliyun ecs DescribeInvocations \
  --RegionId cn-hangzhou \
  --InvokeId t-bp1234567890example
aliyun ecs DescribeInvocationResults \
  --RegionId cn-hangzhou \
  --InvokeId t-bp1234567890example
```

ecctl:

```bash
ecctl ecs command get \
  --region cn-hangzhou \
  --invocation-id t-bp1234567890example \
  --with-results
```

OpenAPI keeps the invocation and result wrappers separate. ecctl merges the
selected result fields into the invocation resource view:

```json
// DescribeInvocations
{"Invocations":{"Invocation":[{"InvokeId":"t-bp1234567890example","CommandId":"c-bp1234567890example","InvokeStatus":"Finished",...}]},...}

// DescribeInvocationResults
{"Invocation":{"InvocationResults":{"InvocationResult":[{"InvokeId":"t-bp1234567890example","InstanceId":"i-bp1234567890example","InvocationStatus":"Success","ExitCode":0,...}]}},...}

// ecctl
{
  "command": {
    "id": "t-bp1234567890example",
    "command_id": "c-bp1234567890example",
    "invoke_id": "t-bp1234567890example",
    "instance": "i-bp1234567890example",
    "status": "Success",
    "exit_code": 0,
    ...
  }
}
```

See the [Cloud Assistant command reference](../../reference/resources/ecs/command.md).

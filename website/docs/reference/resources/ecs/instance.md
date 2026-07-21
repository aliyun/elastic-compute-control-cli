---
generated: true
generated_by: "website/scripts/gen-reference.mjs"
generated_command: "make build && npm --prefix website run gen:reference"
title: ecs instance
sidebar_label: instance
description: "Manage instance resources"
---

# ecs instance

Manage instance resources

Run `ecctl ecs instance <action> -h` for usage, or `ecctl schema ecs.instance.<action> --full` for the complete, agent-readable spec of every parameter and behavior.

## create

```bash
ecctl ecs instance create [flags]
```

Create instance

- Kind: `mutation` · Risk: medium
- Synchronous: waits for `Running` (waiter `running_after_create`, timeout `300s`); use `--no-wait` to skip.
- Idempotent via `ClientToken`.
- Dry run: supported via `--dry-run`.

| API | When called | Purpose |
|---|---|---|
| `DescribeImages` | When `--image` is not empty and does not end with `.vhd`. | Resolve the requested image name to an image ID before creating the instance. |
| `RunInstances` | Every time the command runs. | Perform the resource operation. |
| `DescribeInstances` | When `--no-wait` is not specified and `--dry-run` is not specified. | Poll until the resource reaches the target state. (repeated) |
| `DescribeInstances` | When `--no-wait` is not specified and `--dry-run` is not specified. | Return the final resource view. (cached; no additional request) |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--image` | string | ✓ | ECS image ID or name |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--sg` | string | ✓ | security group ID |
| `--type` | string | ✓ | instance type |
| `--vswitch` | string | ✓ | VSwitch ID |
| `--affinity` | string |  | instance affinity |
| `--amount` | integer |  | number of instances to create |
| `--arn` | object |  | RAM role ARNs for the instance. |
| `--auto-pay` | boolean |  | automatically pay for prepaid instances |
| `--auto-release-time` | string |  | automatic release time |
| `--auto-renew` | boolean |  | automatically renew prepaid instances |
| `--auto-renew-period` | integer |  | automatic renewal period |
| `--clock-options` | object |  | clock options |
| `--cpu-options` | object |  | CPU options |
| `--credit-specification` | string |  | burstable instance credit specification |
| `--data-disk` | object |  | Data disks to attach when creating the instance. |
| `--dedicated-host` | string |  | dedicated host ID |
| `--deletion-protection` | boolean |  | enable deletion protection |
| `--deployment-set` | string |  | deployment set ID |
| `--deployment-set-group-no` | integer |  | deployment set group number |
| `--description` | string |  | instance description |
| `--hibernation-options` | object |  | hibernation options |
| `--host-name` | string |  | host name |
| `--host-names` | array |  | host names |
| `--hpc-cluster` | string |  | HPC cluster ID |
| `--http-endpoint` | string |  | metadata service endpoint setting |
| `--http-put-response-hop-limit` | integer |  | metadata service PUT response hop limit |
| `--http-tokens` | string |  | metadata service token requirement |
| `--image-family` | string |  | image family |
| `--image-options` | object |  | image options |
| `--instance-charge-type` | string |  | instance charge type (default: `PostPaid`) |
| `--internet-bandwidth-in` | integer |  | public inbound bandwidth |
| `--internet-bandwidth-out` | integer |  | public outbound bandwidth |
| `--internet-charge-type` | string |  | internet charge type |
| `--io-optimized` | string |  | I/O optimized setting |
| `--ipv6-address-count` | integer |  | number of IPv6 addresses |
| `--ipv6-addresses` | array |  | IPv6 addresses |
| `--isp` | string |  | line operator |
| `--key-pair` | string |  | key pair name |
| `--launch-template` | string |  | launch template ID |
| `--launch-template-name` | string |  | launch template name |
| `--launch-template-version` | integer |  | launch template version |
| `--min-amount` | integer |  | minimum number of instances to create |
| `--name` | string |  | instance name |
| `--network-interface` | object |  | Elastic network interfaces. |
| `--network-interface-queue-number` | integer |  | network interface queue number |
| `--network-options` | object |  | network options |
| `--password` | string |  | instance password |
| `--password-inherit` | boolean |  | inherit image password |
| `--period` | integer |  | prepaid period |
| `--period-unit` | string |  | prepaid period unit |
| `--private-dns-name-options` | object |  | private DNS name options |
| `--private-ip` | string |  | private IPv4 address |
| `--private-pool-options` | object |  | private pool options |
| `--ram-role` | string |  | RAM role name |
| `--resource-group` | string |  | resource group ID |
| `--scheduler-options` | object |  | scheduler options |
| `--security-enhancement-strategy` | string |  | security enhancement strategy |
| `--security-group-ids` | array |  | security group IDs |
| `--security-options` | object |  | security options |
| `--spot-duration` | integer |  | preemptible instance duration |
| `--spot-interruption-behavior` | string |  | preemptible interruption behavior |
| `--spot-price-limit` | number |  | preemptible instance price limit |
| `--spot-strategy` | string |  | preemptible instance strategy |
| `--storage-set` | string |  | storage set ID |
| `--storage-set-partition-number` | integer |  | storage set partition number |
| `--system-disk` | object |  | System disk configuration. |
| `--tag` | key_value |  | tag assignment key=value |
| `--tenancy` | string |  | tenancy |
| `--unique-suffix` | boolean |  | add a unique suffix to generated names |
| `--user-data` | string |  | user data |
| `--zone` | string |  | zone ID |

## update

```bash
ecctl ecs instance update <id> [flags]
```

Update instance

- Kind: `mutation` · Risk: medium

| API | When called | Purpose |
|---|---|---|
| `ModifyInstanceAttribute` | When `--name` is specified or `--description` is specified or `--host-name` is specified. | Perform the resource operation. |
| `ModifyInstanceAutoReleaseTime` | When `--auto-release-time` is specified. | Perform the resource operation. |
| `ModifyInstanceAutoRenewAttribute` | When `--auto-renew` is specified or `--auto-renew-period` is specified. | Perform the resource operation. |
| `ModifyInstanceChargeType` | When `--instance-charge-type` is specified and `--type` is not specified. | Perform the resource operation. |
| `ModifyInstanceClockOptions` | When `--clock-options` is specified. | Perform the resource operation. |
| `ModifyInstanceMaintenanceAttributes` | When `--maintenance-options` is specified. | Perform the resource operation. |
| `ModifyInstanceMetadataOptions` | When `--http-endpoint` is specified or `--http-tokens` is specified or `--http-put-response-hop-limit` is specified. | Perform the resource operation. |
| `ModifyInstanceNetworkOptions` | When `--network-options` is specified. | Perform the resource operation. |
| `ModifyInstanceNetworkSpec` | When `--internet-bandwidth-in` is specified or `--internet-bandwidth-out` is specified or `--internet-charge-type` is specified. | Perform the resource operation. |
| `ModifyInstanceSpec` | When `--type` is specified and `--instance-charge-type` does not equal `PrePaid`. | Perform the resource operation. |
| `ModifyPrepayInstanceSpec` | When `--type` is specified and `--instance-charge-type` equals `PrePaid`. | Perform the resource operation. |
| `ModifyInstanceVncPasswd` | When `--vnc-password` is specified. | Perform the resource operation. |
| `ModifyInstanceVpcAttribute` | When `--vswitch` is specified or `--private-ip` is specified. | Perform the resource operation. |
| `AllocatePublicIpAddress` | When `--allocate-public-ip` is specified. | Perform the resource operation. |
| `ReplaceSystemDisk` | When `--image` is specified or `--system-disk` is specified. | Perform the resource operation. |
| `AttachInstanceRamRole` | When `--ram-role` is explicitly set to a non-empty value. | Perform the resource operation. |
| `DetachInstanceRamRole` | When `--ram-role` is explicitly set to an empty value. | Perform the resource operation. |
| `AttachKeyPair` | When `--key-pair` is explicitly set to a non-empty value. | Perform the resource operation. |
| `DetachKeyPair` | When `--key-pair` is explicitly set to an empty value. | Perform the resource operation. |
| `DescribeInstanceAttribute` | When `--security-group-ids` is specified. | Read the resource view. |
| `JoinSecurityGroup` | When `--security-group-ids` is specified. | Perform the resource operation. |
| `LeaveSecurityGroup` | When `--security-group-ids` is specified. | Perform the resource operation. |
| `JoinResourceGroup` | When `--resource-group` is specified. | Perform the resource operation. |
| `TagResources` | When `--tag` is specified. | Perform the resource operation. |
| `UntagResources` | When `--remove-tag` is specified. | Perform the resource operation. |
| `DescribeInstances` | When `--no-wait` is not specified. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--auto-renew` | boolean |  | automatically renew prepaid instances |
| `--clock-options` | object |  | clock options |
| `--description` | string |  | instance description |
| `--host-name` | string |  | host name |
| `--http-endpoint` | string |  | metadata service endpoint setting |
| `--http-tokens` | string |  | metadata service token requirement |
| `--image` | string |  | ECS image ID or name |
| `--instance-charge-type` | string |  | instance charge type (default: ``) |
| `--internet-bandwidth-out` | integer |  | public outbound bandwidth |
| `--key-pair` | string |  | key pair name |
| `--maintenance-options` | object |  | maintenance attributes |
| `--name` | string |  | instance name |
| `--network-options` | object |  | network options |
| `--period` | integer |  | prepaid period |
| `--ram-role` | string |  | RAM role name |
| `--remove-tag` | string_array |  | tag keys to remove |
| `--resource-group` | string |  | resource group ID |
| `--security-group-ids` | array |  | security group IDs |
| `--tag` | key_value |  | tag assignment key=value |
| `--type` | string |  | instance type |
| `--vswitch` | string |  | VSwitch ID |

## delete

```bash
ecctl ecs instance delete [<ids>...] [flags]
```

Delete instance

- Kind: `mutation` · Risk: high
- Synchronous: waits for `absent` (waiter `deleted_after_delete`, timeout `300s`); use `--no-wait` to skip.

| API | When called | Purpose |
|---|---|---|
| `DeleteInstance` | When exactly one `&lt;ids>` value is provided. | Perform the resource operation. |
| `DescribeInstances` | When exactly one `&lt;ids>` value is provided and `--no-wait` is not specified. | Poll until the resource reaches the target state. (repeated) |
| `DeleteInstances` | When multiple `&lt;ids>` values are provided. | Perform the resource operation. |
| `DescribeInstances` | When multiple `&lt;ids>` values are provided and `--no-wait` is not specified. | Poll until the resource reaches the target state. (repeated) |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--force` | boolean |  | force release running instance (must be set explicitly) (default: `false`) |

## get

```bash
ecctl ecs instance get <id> [flags]
```

Get instance

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `DescribeInstanceAttribute` | Every time the command runs. | Read the resource view. |
| `DescribeInstanceAutoRenewAttribute` | When `--with-auto-renew` is specified. | Read the resource view. |
| `DescribeInstanceMaintenanceAttributes` | When `--with-maintenance` is specified. | Read the resource view. |
| `DescribeInstanceRamRole` | When `--with-ram-role` is specified. | Read the resource view. |
| `DescribeUserData` | When `--with-user-data` is specified. | Read the resource view. |
| `DescribeInstanceVncUrl` | When `--with-vnc-url` is specified. | Read the resource view. |
| `DescribeCloudAssistantStatus` | When `--with-assistant` is specified. | Read the resource view. |
| `ListPluginStatus` | When `--with-plugin-status` is specified. | Read the resource view. |
| `ListTagResources` | When `--with-tags` is specified. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | comma-separated resource fields to include |
| `--with-assistant` | boolean |  | include cloud assistant status |
| `--with-auto-renew` | boolean |  | include automatic renewal settings |
| `--with-maintenance` | boolean |  | include maintenance attributes |
| `--with-plugin-status` | boolean |  | include cloud assistant plugin status |
| `--with-ram-role` | boolean |  | include RAM role information |
| `--with-tags` | boolean |  | include resource tags |
| `--with-user-data` | boolean |  | include user data |
| `--with-vnc-url` | boolean |  | include VNC login URL |

## list

```bash
ecctl ecs instance list [<ids>...] [flags]
```

List instance resources

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `DescribeInstances` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | comma-separated resource fields to include |
| `--filter` | key_value |  | filter expression key=value |
| `--limit` | integer |  | maximum resources to return (default: `100`) |
| `--next-token` | string |  | token for the next result page |

## exec

```bash
ecctl ecs instance exec [<ids>...] [flags]
```

Run a temporary command on instances

- Kind: `mutation` · Risk: high
- Synchronous: waits for `Success` (waiter `command_invocation_success`, timeout `600s`); use `--no-wait` to skip.

| API | When called | Purpose |
|---|---|---|
| `RunCommand` | Every time the command runs. | Perform the resource operation. |
| `DescribeInvocations` | When `--no-wait` is not specified. | Poll until the resource reaches the target state. (repeated) |
| `DescribeInvocationResults` | When `--no-wait` is not specified. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--command` | string | ✓ | command content to run on the instance |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--command-timeout` | integer |  | command timeout in seconds |
| `--command-type` | string |  | cloud assistant command type (default: `RunShellScript`) |
| `--working-dir` | string |  | command working directory |

## monitor

```bash
ecctl ecs instance monitor <id> [flags]
```

Query instance monitor data

- Kind: `read` · Risk: low

| API | When called | Purpose |
|---|---|---|
| `DescribeInstanceMonitorData` | Every time the command runs. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--end-time` | string | ✓ | monitor query end time |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--start-time` | string | ✓ | monitor query start time |
| `--monitor-period` | integer |  | monitor data sampling period in seconds |

## reboot

```bash
ecctl ecs instance reboot [<ids>...] [flags]
```

Reboot instance

- Kind: `mutation` · Risk: medium
- Synchronous: waits for `Running` (waiter `running_after_reboot`, timeout `300s`); use `--no-wait` to skip.

| API | When called | Purpose |
|---|---|---|
| `RebootInstance` | When exactly one `&lt;ids>` value is provided. | Perform the resource operation. |
| `DescribeInstances` | When exactly one `&lt;ids>` value is provided and `--no-wait` is not specified. | Poll until the resource reaches the target state. (repeated) |
| `RebootInstances` | When multiple `&lt;ids>` values are provided. | Perform the resource operation. |
| `DescribeInstances` | When multiple `&lt;ids>` values are provided and `--no-wait` is not specified. | Poll until the resource reaches the target state. (repeated) |
| `DescribeInstances` | When `--no-wait` is not specified. | Return the final resource view. (cached; no additional request) |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |

## renew

```bash
ecctl ecs instance renew <id> [flags]
```

Renew instance

- Kind: `mutation` · Risk: medium

| API | When called | Purpose |
|---|---|---|
| `RenewInstance` | Every time the command runs. | Perform the resource operation. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--period` | integer | ✓ | prepaid period |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--period-unit` | string |  | prepaid period unit |

## sendfile

```bash
ecctl ecs instance sendfile [<ids>...] [flags]
```

Send a file to instances

- Kind: `mutation` · Risk: high
- Synchronous: waits for `Success` (waiter `sendfile_result_success`, timeout `600s`); use `--no-wait` to skip.

| API | When called | Purpose |
|---|---|---|
| `SendFile` | Every time the command runs. | Perform the resource operation. |
| `DescribeSendFileResults` | When `--no-wait` is not specified. | Poll until the resource reaches the target state. (repeated) |
| `DescribeSendFileResults` | When `--no-wait` is not specified. | Read the resource view. |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--content` | string | ✓ | file content to send |
| `--file-name` | string | ✓ | file name to create |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--target-dir` | string | ✓ | target directory for the file |
| `--file-mode` | string |  | file permission mode |
| `--group` | string |  | file group |
| `--owner` | string |  | file owner |

## start

```bash
ecctl ecs instance start [<ids>...] [flags]
```

Start instance

- Kind: `mutation` · Risk: medium
- Synchronous: waits for `Running` (waiter `running_after_start`, timeout `300s`); use `--no-wait` to skip.

| API | When called | Purpose |
|---|---|---|
| `StartInstance` | When exactly one `&lt;ids>` value is provided. | Perform the resource operation. |
| `DescribeInstances` | When exactly one `&lt;ids>` value is provided and `--no-wait` is not specified. | Poll until the resource reaches the target state. (repeated) |
| `StartInstances` | When multiple `&lt;ids>` values are provided. | Perform the resource operation. |
| `DescribeInstances` | When multiple `&lt;ids>` values are provided and `--no-wait` is not specified. | Poll until the resource reaches the target state. (repeated) |
| `DescribeInstances` | When `--no-wait` is not specified. | Return the final resource view. (cached; no additional request) |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |

## stop

```bash
ecctl ecs instance stop [<ids>...] [flags]
```

Stop instance

- Kind: `mutation` · Risk: medium
- Synchronous: waits for `Stopped` (waiter `stopped_after_stop`, timeout `300s`); use `--no-wait` to skip.

| API | When called | Purpose |
|---|---|---|
| `StopInstance` | When exactly one `&lt;ids>` value is provided. | Perform the resource operation. |
| `DescribeInstances` | When exactly one `&lt;ids>` value is provided and `--no-wait` is not specified. | Poll until the resource reaches the target state. (repeated) |
| `StopInstances` | When multiple `&lt;ids>` values are provided. | Perform the resource operation. |
| `DescribeInstances` | When multiple `&lt;ids>` values are provided and `--no-wait` is not specified. | Poll until the resource reaches the target state. (repeated) |
| `DescribeInstances` | When `--no-wait` is not specified. | Return the final resource view. (cached; no additional request) |

| Parameter | Type | Required | Description |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |

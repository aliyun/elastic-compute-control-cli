---
generated: true
generated_by: "website/scripts/gen-reference.mjs"
generated_command: "make build && npm --prefix website run gen:reference"
title: Products
description: "Browse every public product and resource supported by ecctl."
---

# Products

Select a product to browse its reference, or open a resource directly.

## [ACK](../category/ack)

Manage Container Service for Kubernetes (ACK) clusters across their full lifecycle, including node pools, addons, kubeconfig access, version upgrades, cluster checks and inspection reports, and alert rules.

**22 resources**

| Resource | Description |
|---|---|
| [ack](./resources/ack/ack.md) | Manage ACK clusters |
| [addon](./resources/ack/addon.md) | Manage ACK cluster addons |
| [alert](./resources/ack/alert.md) | Manage ACK alert rule state and contact-group bindings |
| [audit](./resources/ack/audit.md) | Manage ACK cluster API Server audit log |
| [check](./resources/ack/check.md) | Manage ACK cluster check reports |
| [inspect config](./resources/ack/inspect-config.md) | Manage cluster inspection config |
| [audit control-plane-log](./resources/ack/audit-control-plane-log.md) | Manage ACK managed cluster control plane component logs |
| [event](./resources/ack/event.md) | Query ACK control-plane events |
| [inspect](./resources/ack/inspect.md) | Manage ACK cluster inspection configs and reports |
| [policy instance](./resources/ack/policy-instance.md) | Manage ACK policy instances in a cluster |
| [kubeconfig](./resources/ack/kubeconfig.md) | Manage ACK kubeconfig credentials |
| [node](./resources/ack/node.md) | Manage ACK cluster nodes |
| [nodepool](./resources/ack/nodepool.md) | Manage ACK nodepool resources |
| [permission](./resources/ack/permission.md) | Manage ACK RAM user and role permissions |
| [policy](./resources/ack/policy.md) | Manage ACK policy catalog entries |
| [region](./resources/ack/region.md) | Query ACK-supported regions |
| [inspect report](./resources/ack/inspect-report.md) | Trigger and query inspection reports |
| [task](./resources/ack/task.md) | Query and control ACK asynchronous tasks |
| [template](./resources/ack/template.md) | Manage ACK orchestration templates |
| [trigger](./resources/ack/trigger.md) | Manage ACK application redeploy triggers |
| [version](./resources/ack/version.md) | Query ACK Kubernetes version metadata |
| [vuls](./resources/ack/vuls.md) | Manage ACK vulnerability scans and vulnerability views |

## [AGENTRUN](../category/agentrun)

Manage AgentRun sandbox templates and isolated sandbox instances.

**2 resources**

| Resource | Description |
|---|---|
| [sandbox](./resources/agentrun/sandbox.md) | Manage isolated AgentRun sandbox instances. |
| [template](./resources/agentrun/template.md) | Manage AgentRun sandbox templates and their MCP service. |

## [ECS](../category/ecs)

Manage Elastic Compute Service (ECS) resources, including instances, block storage disks and snapshots, images, security groups, elastic network interfaces, key pairs, launch templates, and Cloud Assistant commands.

**16 resources**

| Resource | Description |
|---|---|
| [assistant](./resources/ecs/assistant.md) | Manage Cloud Assistant service settings and agent installation |
| [auto-snapshot-policy](./resources/ecs/auto-snapshot-policy.md) | Manage automatic snapshot policies |
| [command](./resources/ecs/command.md) | Manage ECS Cloud Assistant command templates and invocations |
| [disk](./resources/ecs/disk.md) | Manage disk resources |
| [eni](./resources/ecs/eni.md) | Manage elastic network interfaces |
| [image](./resources/ecs/image.md) | Manage ECS image resources |
| [instance](./resources/ecs/instance.md) | Manage instance resources |
| [keypair](./resources/ecs/keypair.md) | Manage SSH key pairs |
| [launch-template](./resources/ecs/launch-template.md) | Manage ECS launch templates |
| [port-range-list](./resources/ecs/port-range-list.md) | Manage ECS port range lists |
| [prefix-list](./resources/ecs/prefix-list.md) | Manage prefix lists |
| [region](./resources/ecs/region.md) | Query ECS regions |
| [sg](./resources/ecs/sg.md) | Manage security group resources |
| [snapshot](./resources/ecs/snapshot.md) | Manage disk snapshots |
| [snapshot-group](./resources/ecs/snapshot-group.md) | Manage snapshot-consistent groups |
| [zone](./resources/ecs/zone.md) | Query ECS zones in a region |

## [LINGJUN](../category/lingjun)

Manage Lingjun clusters, node groups, and high-performance network resources.

**6 resources**

| Resource | Description |
|---|---|
| [cluster](./resources/lingjun/cluster.md) | Manage Lingjun cluster resources |
| [eni](./resources/lingjun/eni.md) | Manage Lingjun elastic network interfaces |
| [er](./resources/lingjun/er.md) | Manage Lingjun Enterprise Router (HUB) resources |
| [node-group](./resources/lingjun/node-group.md) | Manage Lingjun node group resources |
| [subnet](./resources/lingjun/subnet.md) | Manage Lingjun subnet resources |
| [vpd](./resources/lingjun/vpd.md) | Manage Lingjun VPD resources |

## [RG](../category/rg)

Manage resource group governance resources

**9 resources**

| Resource | Description |
|---|---|
| [admin-setting](./resources/rg/admin-setting.md) | Manage resource group administrator settings |
| [associated-transfer](./resources/rg/associated-transfer.md) | Manage associated resource follow transfer group settings |
| [group](./resources/rg/group.md) | Manage resource groups |
| [notification](./resources/rg/notification.md) | Manage resource group event notifications |
| [policy](./resources/rg/policy.md) | Manage resource group policies |
| [resource](./resources/rg/resource.md) | Manage resources across resource groups |
| [role](./resources/rg/role.md) | Manage RAM roles |
| [service-linked-role](./resources/rg/service-linked-role.md) | Manage service-linked roles |
| [policy version](./resources/rg/policy-version.md) | Manage policy versions |

## [TAG](../category/tag)

Manage tag governance resources

**3 resources**

| Resource | Description |
|---|---|
| [associated-resource-rule](./resources/tag/associated-resource-rule.md) | Manage associated resource tag rules |
| [policy](./resources/tag/policy.md) | Manage tag policies |
| [resource](./resources/tag/resource.md) | Manage tags on cross-product resources |

## [VPC](../category/vpc)

Manage Virtual Private Cloud (VPC) networking, including isolated VPCs and the vSwitches that carve them into zone-level subnets for IP address planning.

**2 resources**

| Resource | Description |
|---|---|
| [vpc](./resources/vpc/vpc.md) | Manage VPC resources |
| [vswitch](./resources/vpc/vswitch.md) | Manage VSwitch resources |

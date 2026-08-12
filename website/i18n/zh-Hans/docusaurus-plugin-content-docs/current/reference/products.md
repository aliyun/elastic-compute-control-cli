---
generated: true
generated_by: "website/scripts/gen-reference.mjs"
generated_command: "make build && npm --prefix website run gen:reference"
title: 产品
description: "浏览 ecctl 支持的全部公开产品和资源。"
---

# 产品

选择产品浏览其参考文档，或直接打开资源参考。

## [ACK](../category/ack)

管理容器服务 Kubernetes 版（ACK）集群的完整生命周期，涵盖节点池、组件（Addon）、kubeconfig 访问、版本升级、集群检查与巡检报告以及告警规则。

**22 个资源**

| 资源 | 描述 |
|---|---|
| [ack](./resources/ack/ack.md) | 管理 ACK 集群 |
| [addon](./resources/ack/addon.md) | 管理 ACK 集群组件 |
| [alert](./resources/ack/alert.md) | 管理 ACK 报警规则状态及联系人组绑定 |
| [audit](./resources/ack/audit.md) | 管理 ACK 集群 API Server 审计日志 |
| [check](./resources/ack/check.md) | 管理 ACK 集群检查报告 |
| [inspect config](./resources/ack/inspect-config.md) | 管理集群巡检配置 |
| [audit control-plane-log](./resources/ack/audit-control-plane-log.md) | 管理 ACK 托管集群控制面组件日志 |
| [event](./resources/ack/event.md) | 查询 ACK 控制面事件 |
| [inspect](./resources/ack/inspect.md) | 管理 ACK 集群巡检配置和报告 |
| [policy instance](./resources/ack/policy-instance.md) | 管理集群中的 ACK 策略实例 |
| [kubeconfig](./resources/ack/kubeconfig.md) | 管理 ACK KubeConfig 凭证 |
| [node](./resources/ack/node.md) | 管理 ACK 集群节点 |
| [nodepool](./resources/ack/nodepool.md) | 管理 ACK 节点池资源 |
| [permission](./resources/ack/permission.md) | 管理 ACK RAM 用户和角色权限 |
| [policy](./resources/ack/policy.md) | 管理 ACK 策略库条目 |
| [region](./resources/ack/region.md) | 查询 ACK 支持的地域 |
| [inspect report](./resources/ack/inspect-report.md) | 触发和查询巡检报告 |
| [task](./resources/ack/task.md) | 查询和控制 ACK 异步任务 |
| [template](./resources/ack/template.md) | 管理 ACK 编排模板 |
| [trigger](./resources/ack/trigger.md) | 管理 ACK 应用重新部署触发器 |
| [version](./resources/ack/version.md) | 查询 ACK Kubernetes 版本元数据 |
| [vuls](./resources/ack/vuls.md) | 管理 ACK 漏洞扫描和漏洞视图 |

## [ECS](../category/ecs)

管理云服务器 ECS 资源，涵盖实例、块存储云盘与快照、镜像、安全组、弹性网卡（ENI）、密钥对、启动模板以及云助手命令。

**16 个资源**

| 资源 | 描述 |
|---|---|
| [assistant](./resources/ecs/assistant.md) | 管理云助手服务配置与 Agent 安装 |
| [auto-snapshot-policy](./resources/ecs/auto-snapshot-policy.md) | 管理自动快照策略 |
| [command](./resources/ecs/command.md) | 管理 ECS 云助手命令模板与执行记录 |
| [disk](./resources/ecs/disk.md) | 管理云盘资源 |
| [eni](./resources/ecs/eni.md) | 管理弹性网卡 |
| [image](./resources/ecs/image.md) | 管理 ECS 镜像资源 |
| [instance](./resources/ecs/instance.md) | 管理实例资源 |
| [keypair](./resources/ecs/keypair.md) | 管理 SSH 密钥对 |
| [launch-template](./resources/ecs/launch-template.md) | 管理 ECS 启动模板 |
| [port-range-list](./resources/ecs/port-range-list.md) | 管理 ECS 端口列表 |
| [prefix-list](./resources/ecs/prefix-list.md) | 管理前缀列表 |
| [region](./resources/ecs/region.md) | 查询 ECS 地域 |
| [sg](./resources/ecs/sg.md) | 管理安全组资源 |
| [snapshot](./resources/ecs/snapshot.md) | 管理云盘快照 |
| [snapshot-group](./resources/ecs/snapshot-group.md) | 管理快照一致性组 |
| [zone](./resources/ecs/zone.md) | 查询地域内的 ECS 可用区 |

## [LINGJUN](../category/lingjun)

管理灵骏集群、节点组及高性能网络资源。

**6 个资源**

| 资源 | 描述 |
|---|---|
| [cluster](./resources/lingjun/cluster.md) | 管理灵骏集群资源 |
| [eni](./resources/lingjun/eni.md) | 管理灵骏弹性网卡 |
| [er](./resources/lingjun/er.md) | 管理灵骏 HUB（Enterprise Router）资源 |
| [node-group](./resources/lingjun/node-group.md) | 管理灵骏节点组资源 |
| [subnet](./resources/lingjun/subnet.md) | 管理灵骏子网资源 |
| [vpd](./resources/lingjun/vpd.md) | 管理灵骏网段资源 |

## [RG](../category/rg)

管理资源组治理资源

**9 个资源**

| 资源 | 描述 |
|---|---|
| [admin-setting](./resources/rg/admin-setting.md) | 管理资源组管理员设置 |
| [associated-transfer](./resources/rg/associated-transfer.md) | 管理关联资源随转组设置 |
| [group](./resources/rg/group.md) | 管理资源组 |
| [notification](./resources/rg/notification.md) | 管理资源组事件通知 |
| [policy](./resources/rg/policy.md) | 管理权限策略 |
| [resource](./resources/rg/resource.md) | 管理资源组中的资源 |
| [role](./resources/rg/role.md) | 管理 RAM 角色 |
| [service-linked-role](./resources/rg/service-linked-role.md) | 管理服务关联角色 |
| [policy version](./resources/rg/policy-version.md) | 管理权限策略版本 |

## [TAG](../category/tag)

管理标签治理资源

**3 个资源**

| 资源 | 描述 |
|---|---|
| [associated-resource-rule](./resources/tag/associated-resource-rule.md) | 管理关联资源标签规则 |
| [policy](./resources/tag/policy.md) | 管理标签策略 |
| [resource](./resources/tag/resource.md) | 管理跨产品资源标签 |

## [VPC](../category/vpc)

管理专有网络 VPC，涵盖隔离的 VPC 网络及其交换机（vSwitch），用于按可用区划分子网与 IP 地址规划。

**2 个资源**

| 资源 | 描述 |
|---|---|
| [vpc](./resources/vpc/vpc.md) | 管理 VPC 资源 |
| [vswitch](./resources/vpc/vswitch.md) | 管理交换机资源 |

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

管理容器服务 Kubernetes 版（ACK）集群的完整生命周期，涵盖节点池、组件（Addon）、kubeconfig 访问、版本升级、诊断巡检与巡检报告以及告警规则。

**7 个资源**

| 资源 | 描述 |
|---|---|
| [ack](./resources/ack/ack.md) | 管理 ACK 集群 |
| [kubeconfig](./resources/ack/kubeconfig.md) | 管理 ACK KubeConfig 凭证 |
| [node](./resources/ack/node.md) | 管理 ACK 集群节点 |
| [nodepool](./resources/ack/nodepool.md) | 管理 ACK 节点池资源 |
| [permission](./resources/ack/permission.md) | 管理 ACK RAM 用户和角色权限 |
| [region](./resources/ack/region.md) | 查询 ACK 支持的地域 |
| [version](./resources/ack/version.md) | 查询 ACK Kubernetes 版本元数据 |

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

通过公开的集群和 VPD 命令管理灵骏智算及高性能网络资源。

**2 个资源**

| 资源 | 描述 |
|---|---|
| [cluster](./resources/lingjun/cluster.md) | 管理灵骏集群资源 |
| [vpd](./resources/lingjun/vpd.md) | 管理灵骏网段资源 |

## [VPC](../category/vpc)

管理专有网络 VPC，涵盖隔离的 VPC 网络及其交换机（vSwitch），用于按可用区划分子网与 IP 地址规划。

**2 个资源**

| 资源 | 描述 |
|---|---|
| [vpc](./resources/vpc/vpc.md) | 管理 VPC 资源 |
| [vswitch](./resources/vpc/vswitch.md) | 管理交换机资源 |

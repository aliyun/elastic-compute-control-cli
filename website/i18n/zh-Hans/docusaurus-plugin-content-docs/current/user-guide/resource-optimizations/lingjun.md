---
title: 灵骏专项优化
description: ecctl 公开命令面中的灵骏集群和 VPD 工作流。
---

# 灵骏专项优化

以下优化适用于灵骏集群和 VPD 网段。文中的资源 ID 和返回值仅用于演示。

## 集群

### 扩缩容工作流路由

灵骏 OpenAPI 将扩容和缩容分别定义为 `ExtendCluster` 和 `ShrinkCluster`。ecctl 使用
同一个 `cluster update` 动作，根据 `--extend` 或 `--shrink` 选择底层操作。同时传入
两种模式时，命令会在发送请求前拒绝执行。

例如，下面两条命令都会为同一个节点组增加一个节点：

阿里云 CLI：

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

缩容时，直接调用方需要改用 `ShrinkCluster`；ecctl 保持同一个资源动作，只改变输入模式：

阿里云 CLI：

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

### 按需查询节点详情

直接调用时，需要先执行 `DescribeCluster`，再单独查询普通节点或超节点并合并响应。
ecctl 只执行 `--with-nodes` 或 `--with-hyper-nodes` 明确选择的详情查询，并返回一个
资源视图。

阿里云 CLI：

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

OpenAPI 分别返回集群属性和节点。ecctl 对请求的节点归一化后，将其合并到集群对象中：

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

这个 ecctl 示例没有传入 `--with-hyper-nodes`，因此不会调用 `ListClusterHyperNodes`。

参见[灵骏集群参考](../../reference/resources/lingjun/cluster.md)。

## VPD

### 等待异步创建完成

`CreateVpd` 会在 VPD 就绪前返回。直接调用方需要保存 VPD ID，并轮询 `GetVpd` 或
`ListVpds`，直到状态变为 `Available`。ecctl 默认完成这些步骤并返回最终资源视图；
需要保留异步行为时，可以使用 `--no-wait`。

阿里云 CLI：

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

原始创建响应包含新资源 ID 和中间状态，ecctl 默认返回进入 `Available` 后的 VPD：

```json
// CreateVpd
{"Content": {"VpdId": "vpd-bp1234567890example", ...}, ...}

// ecctl
{"vpd": {"id": "vpd-bp1234567890example", "status": "Available", ...}, ...}
```

### 按需查询路由和授权

VPD 属性、路由条目和授权规则来自不同的 OpenAPI。ecctl 只合并调用方明确请求的
关联数据。

阿里云 CLI：

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

路由列表在 OpenAPI 中是独立响应。ecctl 将它归一化到 VPD 对象下：

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

这个 ecctl 命令不会调用 `ListVpdGrantRules`；还需要授权规则时，再增加 `--with-grants`。

### 辅助网段变更

VPD 主网段不可修改。OpenAPI 使用不同操作关联和取消关联辅助网段，ecctl 则在一个
资源更新动作中使用 `+` 或 `-` 前缀表达两种变更。

阿里云 CLI：

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

删除同一个辅助网段时，直接调用 `UnAssociateVpdCidrBlock`，或通过 ecctl 传入
`--cidr -172.16.0.0/16`。

参见[灵骏 VPD 参考](../../reference/resources/lingjun/vpd.md)。

---
title: ACK 专项优化
description: ecctl 的 ACK 集群、节点池、kubeconfig、权限和版本工作流。
---

# ACK 专项优化

以下资源 ID 和返回值仅用于演示。执行命令时，请替换为当前账号下的真实值。

## 集群

### 将一个更新动作路由到所需 API

ACK 使用不同 OpenAPI 处理集群配置、版本规格迁移、标签替换、增加标签和删除标签。
ecctl 将这些变更统一放在 `ack update` 下，并根据传入字段选择底层操作。互相冲突的
标签模式会在执行前被拒绝。

例如，修改集群名称时，直接调用 `ModifyCluster`，通过 ecctl 则传入 `--name`：

阿里云 CLI：

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

版本规格迁移使用另一个 OpenAPI，但 ecctl 的资源动作保持不变：

阿里云 CLI：

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

### 按需合并集群详情

集群详情、云资源、标签和策略治理数据来自不同的 ACK API。直接调用时，需要分别执行
这些 API 并合并响应。ecctl 只执行 `--with-resources`、`--with-tags` 和
`--with-policy-governance` 明确选择的详情查询。

阿里云 CLI：

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

OpenAPI 的响应相互独立，ecctl 则将所选详情放在同一个 `cluster` 对象中：

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

这个 ecctl 命令没有传入 `--with-policy-governance`，因此不会调用
`DescribePolicyGovernanceInCluster`。

### 选择账号或地域列表 API

ACK 使用 `DescribeClustersV1` 获取默认账号列表，使用 `DescribeClustersForRegion`
处理地域跨账号模式。直接调用时，需要自行选择操作。ecctl 根据 `--cross-account`
选择 API，同时保持相同的列表命令。

阿里云 CLI：

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

地域跨账号列表使用下面这组命令：

阿里云 CLI：

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

参见 [ACK 集群参考](../../reference/resources/ack/ack.md)。

## 节点池

### 根据输入路由节点池变更

节点池配置、期望节点数、节点配置和标签分别使用不同的 ACK 操作。ecctl 将这些操作
统一放在 `nodepool update` 下，只执行传入字段选择的工作流。`--with-node-config`
用于显式启用节点级配置。

例如，直接扩缩容需要调用 `ScaleClusterNodePool` 并构造请求体，ecctl 则将期望节点数
作为资源字段：

阿里云 CLI：

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

只有同时提供模式和配置时，节点配置变更才会路由到 `ModifyNodePoolNodeConfig`：

阿里云 CLI：

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

### 选择节点修复或漏洞修复

ACK 将节点修复和漏洞修复分别定义为 `RepairClusterNodePool` 和 `FixNodePoolVuls`。
ecctl 根据 `--node` 或 `--vulnerabilities` 选择工作流，两种模式不能同时使用。

阿里云 CLI：

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

改为传入 `--node node-bp1234567890example` 时，ecctl 会路由到
`RepairClusterNodePool`。

### 挂载实例或只返回挂载脚本

ACK 使用 `AttachInstancesToNodePool` 挂载 ECS 实例，使用
`DescribeClusterAttachScripts` 生成挂载脚本。ecctl 将两种结果统一放在
`nodepool attach` 下。`--print-script-only` 只返回脚本，不会挂载实例。

阿里云 CLI：

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

需要返回脚本时，直接调用另一个 OpenAPI，或切换 ecctl 模式：

阿里云 CLI：

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

参见[节点池参考](../../reference/resources/ack/nodepool.md)。

## Kubeconfig

### 选择集群所有者或子账号配置

ACK 为当前集群所有者和 RAM 子账号提供不同的 API。直接调用时，需要选择对应操作，
再从响应中读取 `config` 和 `expiration`。ecctl 在传入 `--user-id` 时选择子账号路径，
并返回归一化的 `kubeconfig` 对象。

阿里云 CLI：

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

省略 `--user-id` 时，ecctl 改用 `DescribeClusterUserKubeconfig`。

### 修改有效期或吊销访问

ACK 使用一个 API 修改子账号 kubeconfig 的有效期，使用另一个 API 吊销当前集群的
kubeconfig。ecctl 将它们定义为明确的资源动作。

阿里云 CLI：

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

吊销访问使用下面这组命令：

阿里云 CLI：

```bash
aliyun cs RevokeK8sClusterKubeConfig \
  --ClusterId c-bp1234567890example
```

ecctl:

```bash
ecctl ack kubeconfig revoke \
  --cluster c-bp1234567890example
```

参见 [Kubeconfig 参考](../../reference/resources/ack/kubeconfig.md)。

## 权限

### 增量更新或全量替换并回读

ACK 使用不同 API 处理权限增量更新和全量替换。直接调用时，需要先选择操作，再执行
`DescribeUserPermission` 读取有效权限。ecctl 根据 `--replace` 选择更新模式，并自动
完成回读。

阿里云 CLI：

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

直接调用会分别返回更新确认和回读结果。ecctl 自动回读后，将生效权限与执行过的动作
一起返回：

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

全量替换时，直接调用方使用 `GrantPermissions`，ecctl 则在同一个资源动作中增加
`--replace`：

阿里云 CLI：

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

### 集群范围或用户范围清理

ACK 将单个集群清理和用户全部集群清理定义为不同操作。直接调用时，需要选择
`CleanClusterUserPermissions` 或 `CleanUserPermissions`，再查询剩余权限。ecctl 要求
显式传入 `--cluster` 或 `--all-clusters`，然后自动回读。

阿里云 CLI：

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

清理全部集群时，直接调用 `CleanUserPermissions`，或将 ecctl 命令中的
`--cluster ...` 替换为 `--all-clusters`。

参见[权限参考](../../reference/resources/ack/permission.md)。

## 版本

### 校验并映射元数据选择条件

`DescribeKubernetesVersionMetadata` 要求传入地域和集群类型。ecctl 可以通过
`--cluster-type` 或 `--filter cluster-type=...` 接收集群类型，并在调用 ACK 前校验
选择条件。

阿里云 CLI：

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

ecctl 命令省略集群类型时，会在发送 OpenAPI 请求前被拒绝。

参见[版本参考](../../reference/resources/ack/version.md)。

---
title: ECS 专项优化
description: ecctl 为 ECS 提供的输入转换、多 API 工作流和输出归一化。
---

# ECS 专项优化

ECS 资源命令将 API 调用转换为资源动作。下面按 ECS 资源说明专项差异，并为每个差异
提供具体的阿里云 CLI 和 ecctl 示例。通用的输出、分页、错误和 schema 契约见
[通用差异](../common-differences.md)。

以下资源 ID 和返回值仅用于演示。执行命令时，请替换为当前账号下的真实值。

## 实例

### 创建前解析镜像名称

`RunInstances.ImageId` 要求镜像 ID。直接调用方只有镜像名称时，需要先查询
`DescribeImages`、选择结果，再将镜像 ID 传给 `RunInstances`。ecctl 会把非空且不以
`.vhd` 结尾的 `--image` 值作为名称，完成查询后将解析出的 ID 传给 `RunInstances`。
以 `.vhd` 结尾的值直接作为 ID 透传。

阿里云 CLI：

```bash
aliyun ecs DescribeImages \
  --RegionId cn-hangzhou \
  --ImageName '*aliyun*' \
  --InstanceType ecs.g6.large
```

如果查询返回 `ImageId=aliyun_4_x64_20G_agentic_alibase_20260704.vhd`，直接创建时
需要使用这个返回值：

阿里云 CLI：

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

解析结果可能随地域、实例规格和时间变化。

### 将字段路由到所属的更新 API

ECS 使用不同 API 管理实例属性、规格、RAM 角色、安全组和标签。直接调用时，需要为
每项变更选择操作。`ecctl ecs instance update` 根据已建模字段选择操作；命令支持时，
仍可用 `--api-param` 补充底层参数。

假设一次维护需要将实例名称改为 `web-01`，同时将实例规格改为 `ecs.g6.large`。这两个
字段分别属于 `ModifyInstanceAttribute` 和 `ModifyInstanceSpec`，因此阿里云 CLI
需要执行两条命令。ecctl 在一个资源更新中接收两个字段，再分别路由到所属 API。
这个示例中的规格变更使用默认后付费路径。

阿里云 CLI：

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

### 对齐期望安全组集合

OpenAPI 使用 `JoinSecurityGroup` 和 `LeaveSecurityGroup` 表达单次加入或移出。直接调用方
需要读取实例当前状态、计算集合差异，再逐项发送请求。ecctl 将
`--security-group-ids` 视为最终期望集合，并自动完成对齐。

假设实例当前属于 `sg-a` 和 `sg-b`，期望集合改为 `sg-b,sg-c` 时，直接调用需要执行：

阿里云 CLI：

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

### 选择单实例或批量动作

ECS 为单实例和批量启动、停止、重启、删除提供不同 API。ecctl 在同一个资源动作中
接收一个或多个 ID，并根据 ID 数量选择 API。

阿里云 CLI：

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

只传一个 ID 时，同一个 ecctl 动作调用 `StartInstance`；像上面一样传入两个 ID 时，
则调用 `StartInstances`。

### 按需合并实例详情

自动续费、维护属性、RAM 角色、UserData、VNC URL、云助手状态、插件状态和标签来自
不同 API。ecctl 只执行 `--with-*` 明确选择的详情查询，并将结果合并到一个实例视图。

阿里云 CLI：

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

阿里云 CLI 返回两个相互独立的响应：

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

ecctl 将所选详情合并到实例视图中：

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

没有传入其他详情参数，因此这个 ecctl 命令不会执行其余可选查询。

### 解码云助手输出

云助手以 Base64 返回命令输出。直接调用方需要自行解码
`DescribeInvocationResults.Output`。`instance exec` 保留原始 `output`，并在解码成功
时增加 `output_text`；无效 Base64 不会作为解码文本返回。

阿里云 CLI：

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

### 为库存和状态错误补充恢复建议

ECS 和阿里云 CLI 返回服务端错误码、消息和 Request ID。ecctl 在 `actions` 中保留这些
字段，并为库存错误增加恢复命令；遇到删除状态错误时，则说明应先停止实例还是强制释放。

阿里云 CLI：

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

实例规格不可用时，关键返回差异如下：

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

参见[实例参考](../../reference/resources/ecs/instance.md)。

## 安全组

### 展开安全组规则简写

ECS 授权 API 要求分别提供协议、端口范围和 CIDR。ecctl 支持
`protocol:port@cidr`、`protocol:port:cidr` 和 `direction:protocol:port:cidr`，并在
调用 API 前展开字段。结构格式错误会在本地被拒绝，CIDR 等字段值仍由服务端校验。

阿里云 CLI：

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

### 归一化协议、策略和端口

直接调用方需要自行构造 `IpProtocol=tcp`、`Policy=accept` 和
`PortRange=100/200` 等 ECS 字段。ecctl 将协议和策略转为小写，并把简写端口转换为
ECS 范围。ICMP 的 `-1` 保留为 `-1/-1`。

阿里云 CLI：

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

### 路由入方向和出方向操作

ECS 为入方向和出方向授权、撤销、修改提供不同 API。ecctl 将方向保留为资源规则字段，
并选择对应操作。入方向使用 `SourceCidrIp`，出方向使用 `DestCidrIp`。

阿里云 CLI：

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

### 按需合并安全组引用

安全组属性和引用关系来自不同 API。直接调用时，需要分别查询并合并结果。只有传入
`--with-references` 时，ecctl 才会调用 `DescribeSecurityGroupReferences`。

阿里云 CLI：

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

两个 OpenAPI 响应相互独立。ecctl 将引用关系合并到安全组视图中：

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

参见[安全组参考](../../reference/resources/ecs/sg.md)。

## 云盘

### 将云盘字段路由到所属 API

云盘属性、容量、性能、计费、部署和默认加密使用不同的 ECS API。直接调用时，需要
自行选择操作。`disk update` 只运行传入字段选择的工作流。

阿里云 CLI：

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

这里 ecctl 将容量变更路由到 `ResizeDisk`，不会调用其他云盘更新 API。

参见[云盘参考](../../reference/resources/ecs/disk.md)。

## 快照

### 将快照变更路由到所属 API

快照属性、分类、锁定和快照服务开通使用不同操作。ecctl 将它们统一放在
`snapshot update` 下，并根据请求字段选择操作。

阿里云 CLI：

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

分类变更会改为路由到 `ModifySnapshotCategory`，锁定或解锁输入则选择 `LockSnapshot`
或 `UnlockSnapshot`。

### 合并可选快照详情

锁定状态、快照链、监控、套餐和用量信息来自不同 API。ecctl 只增加 `--with-*`
明确选择的详情。

阿里云 CLI：

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

锁定信息在 OpenAPI 中是独立响应，ecctl 则将它添加到快照对象中：

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

增加 `--with-usage` 时，ecctl 还会执行用量查询；省略该参数则不会调用。

参见[快照参考](../../reference/resources/ecs/snapshot.md)。

## 镜像

### 路由属性和共享权限变更

镜像属性和共享权限使用不同的 ECS API。ecctl 根据更新字段选择操作。

阿里云 CLI：

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

改为传入共享权限字段时，ecctl 会路由到 `ModifyImageSharePermission`。

### 合并可选镜像详情

共享权限和支持的实例规格来自不同查询。只有请求 `--with-share-permission` 或
`--with-supported-instance-types` 时，ecctl 才会调用相应 API。

阿里云 CLI：

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

支持的实例规格来自独立的 OpenAPI 响应。ecctl 将归一化后的规格列表放到镜像对象中：

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

参见[镜像参考](../../reference/resources/ecs/image.md)。

## 自动快照策略

### 合并策略与云盘关联变更

ECS 使用不同 API 修改自动快照策略、应用云盘关联和取消云盘关联。一个 ecctl
`update` 动作可以完成所选变更，每个字段只触发对应操作。

阿里云 CLI：

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

参见[自动快照策略参考](../../reference/resources/ecs/auto-snapshot-policy.md)。

## 弹性网卡

### 将网卡变更路由到地址和 QoS API

弹性网卡属性、私网 IPv4 地址、IPv6 地址、前缀和 QoS 使用不同操作。ecctl 将它们
统一放在 `eni update` 下，只执行调用方传入的变更。

阿里云 CLI：

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

使用 `-` 前缀时，地址删除会路由到 `UnassignPrivateIpAddresses`；属性或 QoS 字段
则选择各自的 API。

参见[弹性网卡参考](../../reference/resources/ecs/eni.md)。

## 前缀列表

### 在一个更新中表达条目增加和删除

`ModifyPrefixList` 使用不同集合表达新增和删除条目。ecctl 通过重复的 `--entry`
接收两种变更，并使用 `+` 或 `-` 前缀区分。

阿里云 CLI：

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

### 合并前缀列表引用

前缀列表条目和引用该列表的资源来自不同查询。只有传入 `--with-associations` 时，
ecctl 才会增加引用数据。

阿里云 CLI：

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

OpenAPI 将条目和引用放在不同的响应包装层中。ecctl 将两者归一化到前缀列表对象下：

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

参见[前缀列表参考](../../reference/resources/ecs/prefix-list.md)。

## 端口列表

### 按需合并条目和引用

端口列表条目和引用资源使用不同的 ECS API。直接调用时，需要分别查询并合并。ecctl
只合并 `--with-entries` 和 `--with-associations` 选择的详情。

阿里云 CLI：

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

OpenAPI 分别返回条目和引用。ecctl 对两个集合归一化后，将它们合并到端口列表对象中：

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

参见[端口列表参考](../../reference/resources/ecs/port-range-list.md)。

## 启动模板

### 创建版本或切换默认版本

ECS 使用 `CreateLaunchTemplateVersion` 创建版本，使用
`ModifyLaunchTemplateDefaultVersion` 修改默认版本。ecctl 将两种模式统一放在
`launch-template update` 下，命令契约会拒绝不兼容的模式。

阿里云 CLI：

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

将默认版本切换为版本 2 时，需要使用另一个 OpenAPI 操作：

阿里云 CLI：

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

### 删除一个版本或整个模板

ECS 分别提供版本删除和模板删除 API。ecctl 使用 `--version` 区分两种结果。

阿里云 CLI：

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

省略 `--version` 时，ecctl 会调用 `DeleteLaunchTemplate` 删除整个模板。

### 按需合并模板版本

模板元数据和版本列表来自不同查询。只有传入 `--with-versions` 时，ecctl 才会调用
`DescribeLaunchTemplateVersions`。

阿里云 CLI：

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

OpenAPI 将模板元数据和版本放在不同的响应包装层中。ecctl 将归一化后的版本合并到
启动模板对象中：

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

参见[启动模板参考](../../reference/resources/ecs/launch-template.md)。

## 密钥对

### 选择新建或导入公钥

ECS 将自动生成密钥对和导入公钥定义为不同操作。传入 `--public-key` 时，ecctl 选择
`ImportKeyPair`；省略该参数时选择 `CreateKeyPair`。

阿里云 CLI：

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

不传 `--public-key` 时，同一个 ecctl 动作会通过 `CreateKeyPair` 生成新密钥对。

参见[密钥对参考](../../reference/resources/ecs/keypair.md)。

## 云助手命令

### 按需合并执行结果

执行记录元数据和每台实例的执行结果来自不同的 ECS API。直接调用时，需要分别查询并
合并。只有请求 `--with-results` 时，ecctl 才会执行结果查询。

阿里云 CLI：

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

OpenAPI 分别保留执行记录和执行结果的包装层。ecctl 将所选结果字段合并到执行记录资源
视图中：

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

参见[云助手命令参考](../../reference/resources/ecs/command.md)。

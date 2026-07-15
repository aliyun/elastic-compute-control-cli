---
title: VPC 专项优化
description: ecctl 为 VPC 和交换机提供的服务端 DryRun、幂等与列表输出归一化。
---

# VPC 专项优化

以下资源 ID 和返回值仅用于演示。执行命令时，请替换为当前账号下的真实值。

## VPC

### 服务端 DryRun

VPC 删除校验通过时，OpenAPI 会返回 `DryRunOperation` 错误哨兵，因此阿里云 CLI
的进程退出码为 1。ecctl 发送相同的服务端校验请求，但会将该哨兵转换为
`dry_run: passed`，进程退出码为 0。两条命令都不会删除 VPC。

阿里云 CLI：

```bash
aliyun vpc DeleteVpc \
  --RegionId cn-hangzhou \
  --VpcId vpc-bp1234567890example \
  --DryRun true
```

ecctl:

```bash
ecctl vpc delete vpc-bp1234567890example \
  --region cn-hangzhou \
  --dry-run
```

关键返回值体现了两种进程契约的差异：

```json
// Alibaba Cloud CLI
ErrorCode: DryRunOperation
Message: Request validation has been passed with DryRun flag set.
...
Exit code: 1

// ecctl
{
  "actions": [{"action_name": "DeleteVpc", ...}],
  "dry_run": "passed",
  ...
}
Exit code: 0
```

### 幂等创建

OpenAPI 和阿里云 CLI 直接提供 `ClientToken`。ecctl 将同一个请求身份命名为
`--idempotency-key`，省略该参数时可以自动生成 Token。如果重试可能由另一个进程
发起，应显式指定并复用同一个值。

阿里云 CLI：

```bash
aliyun vpc CreateVpc \
  --RegionId cn-hangzhou \
  --VpcName prod-vpc \
  --CidrBlock 10.0.0.0/16 \
  --ClientToken provisioning-42
```

ecctl:

```bash
ecctl vpc create \
  --region cn-hangzhou \
  --name prod-vpc \
  --cidr 10.0.0.0/16 \
  --idempotency-key provisioning-42
```

使用相同 Token 重复执行任一命令，都会复用同一个服务端请求身份。

### 列表输出归一化

`DescribeVpcs` 将 VPC 放在 `Vpcs.Vpc` 包装层下，并返回 API 专用分页字段。
`ecctl vpc list` 将相同数据映射为 `vpcs` 数组和统一分页信息。最大分页大小仍由
`DescribeVpcs` 决定。

阿里云 CLI：

```bash
aliyun vpc DescribeVpcs \
  --RegionId cn-hangzhou \
  --PageNumber 1 \
  --PageSize 50
```

ecctl:

```bash
ecctl vpc list \
  --region cn-hangzhou \
  --filter status=Available \
  --page 1 \
  --limit 50
```

```json
// OpenAPI
{
  "Vpcs": {"Vpc": [{"VpcId": "vpc-bp1234567890example", "Status": "Available", ...}]},
  "PageNumber": 1,
  "PageSize": 50,
  "TotalCount": 1,
  ...
}

// ecctl
{
  "vpcs": [{"id": "vpc-bp1234567890example", "status": "Available", ...}],
  "pagination": {"page": 1, "limit": 50, "returned": 1, "has_more": false},
  ...
}
```

参见 [VPC 参考](../../reference/resources/vpc/vpc.md)。

## 交换机

### 服务端 DryRun

交换机删除校验通过时，OpenAPI 使用 `DryRunOperation` 错误哨兵。ecctl 将相同响应
转换为成功的 DryRun 结果，并且不会删除交换机。

阿里云 CLI：

```bash
aliyun vpc DeleteVSwitch \
  --RegionId cn-hangzhou \
  --VSwitchId vsw-bp1234567890example \
  --DryRun true
```

ecctl:

```bash
ecctl vpc vswitch delete vsw-bp1234567890example \
  --region cn-hangzhou \
  --dry-run
```

```json
// Alibaba Cloud CLI
ErrorCode: DryRunOperation
...
Exit code: 1

// ecctl
{"actions": [{"action_name": "DeleteVSwitch", ...}], "dry_run": "passed", ...}
Exit code: 0
```

### 幂等创建

阿里云 CLI 要求直接传入 `ClientToken`。ecctl 将同一个值暴露为
`--idempotency-key`，省略时可以自动生成。跨进程重试时，应复用显式幂等键。

阿里云 CLI：

```bash
aliyun vpc CreateVSwitch \
  --RegionId cn-hangzhou \
  --VpcId vpc-bp1234567890example \
  --ZoneId cn-hangzhou-h \
  --CidrBlock 10.0.1.0/24 \
  --ClientToken app-a-42
```

ecctl:

```bash
ecctl vpc vswitch create \
  --region cn-hangzhou \
  --vpc vpc-bp1234567890example \
  --zone cn-hangzhou-h \
  --cidr 10.0.1.0/24 \
  --idempotency-key app-a-42
```

### 列表输出归一化

`DescribeVSwitches` 将交换机放在 `VSwitches.VSwitch` 包装层下，并使用专用分页
字段。ecctl 将匹配的交换机放在 `vswitches` 下，并返回统一的 `pagination` 对象。
最大分页大小仍由服务端决定。

阿里云 CLI：

```bash
aliyun vpc DescribeVSwitches \
  --RegionId cn-hangzhou \
  --VpcId vpc-bp1234567890example \
  --PageNumber 1 \
  --PageSize 50
```

ecctl:

```bash
ecctl vpc vswitch list \
  --region cn-hangzhou \
  --filter vpc=vpc-bp1234567890example \
  --page 1 \
  --limit 50
```

```json
// OpenAPI
{
  "VSwitches": {"VSwitch": [{"VSwitchId": "vsw-bp1234567890example", ...}]},
  "PageNumber": 1,
  "PageSize": 50,
  ...
}

// ecctl
{
  "vswitches": [{"id": "vsw-bp1234567890example", ...}],
  "pagination": {"page": 1, "limit": 50, "returned": 1, "has_more": false},
  ...
}
```

参见[交换机参考](../../reference/resources/vpc/vswitch.md)。

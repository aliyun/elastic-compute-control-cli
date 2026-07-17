---
title: 通用差异
description: 比较 ecctl 资源命令、阿里云 CLI 和直接 OpenAPI 调用。
---

# 通用差异

`ecctl`、阿里云 CLI 和直接 OpenAPI 都能访问相同的云服务，但使用方式和返回结果不同。阿里云 CLI 基本沿用 API 操作和参数；直接调用 OpenAPI 还要处理签名、端点和请求序列化；ecctl 则把已公开的操作整理为资源命令。

## 快速比较

| 方面 | ecctl 资源命令 | 阿里云 CLI 或直接 OpenAPI |
|---|---|---|
| 命令 | 资源和动作，例如 `ecs instance create` | 产品和 API，例如 `ecs RunInstances` |
| 输入 | `--type`、`--image`、`--sg` 等资源字段 | `InstanceType`、`ImageId`、`SecurityGroupId` 等 OpenAPI 字段 |
| 工作流 | 可以调用多个 API、等待并查询最新资源 | 通常执行一个 API；通用轮询需要另外配置 |
| 输出 | 整理后的资源信息，字段使用 snake_case | API 原始响应及包装字段 |
| 错误 | 结构化错误，并记录实际调用的 API | 当前 API 或 CLI 调用返回的错误 |
| 发现 | `capabilities` 和命令级 `schema` | API help 和元数据 |
| 覆盖 | 已提供资源命令的操作 | 覆盖更广的 OpenAPI |

## 资源化命令

ecctl 的命令格式为 `product/[parent/]resource/action`，只有嵌套资源才包含父资源。资源 ID 通常作为位置参数传入，多个相关 API 可以由一个资源动作完成。例如，`ecs instance update` 会根据输入字段选择相应的 API，用来修改实例属性、网络、安全组、标签或关联身份。

需要指定某个 API 及其完整请求结构时，阿里云 CLI 更合适。ecctl 命令语法见[命令模型](./command-model.md)。

例如，阿里云 CLI 使用 `ModifyInstanceAttribute` 操作和 OpenAPI 参数名修改实例名称：

```bash
aliyun ecs ModifyInstanceAttribute \
  --region cn-hangzhou \
  --InstanceId i-bp1234567890example \
  --InstanceName web-02
```

对应的 ecctl 命令直接指定资源和动作，实例 ID 作为位置参数传入，名称使用 `--name`：

```bash
ecctl ecs instance update i-bp1234567890example \
  --region cn-hangzhou \
  --name web-02
```

## 资源化输入

ecctl 将 OpenAPI 名称映射为更短的资源字段和 kebab-case 参数。创建 ECS 实例时，
`InstanceType`、`ImageId`、`SecurityGroupId` 和 `VSwitchId` 分别映射为
`--type`、`--image`、`--sg` 和 `--vswitch`。

下面两条命令传入相同的资源信息。其中的资源 ID 仅用于演示，请替换为当前账号下的资源。
阿里云 CLI 保留 OpenAPI 参数名，并将标签拆成 Key 和 Value 两个字段：

```bash
aliyun ecs RunInstances \
  --RegionId cn-hangzhou \
  --InstanceType ecs.e3.medium \
  --ImageId aliyun_3_x64_20G_alibase_20240528.vhd \
  --SecurityGroupId sg-bp1234567890example \
  --VSwitchId vsw-bp1234567890example \
  --InstanceName web-01 \
  --Tag.1.Key env \
  --Tag.1.Value prod
```

ecctl 使用资源字段，并将标签作为一个 `key=value` 输入：

```bash
ecctl ecs instance create \
  --region cn-hangzhou \
  --type ecs.e3.medium \
  --image aliyun_3_x64_20G_alibase_20240528.vhd \
  --sg sg-bp1234567890example \
  --vswitch vsw-bp1234567890example \
  --name web-01 \
  --tag env=prod
```

筛选条件使用 `--filter key=value`，标签使用 `--tag key=value`。命令支持时，结构化对象可以使用内联值、JSON 或 `@file.json`。部分资源还会对输入做转换，比如解析 ECS 镜像名称和安全组规则简写。这些转换统一列在[资源专项优化](./resource-optimizations/index.md)中。

## 多 API 工作流与等待

资源命令可以先调用变更 API，再轮询查询 API，等待资源到达目标状态，最后返回最新资源信息。是否等待以及如何等待，由各命令定义。使用 `--no-wait` 可在变更 API 返回后立即结束，使用 `--timeout` 可调整等待上限。

阿里云 CLI 提供通用等待器（waiter），但调用方需要给出查询命令、表达式、目标值和时间配置。ecctl 的 `schema` 会直接列出轮询所用的查询操作、目标状态、失败状态和超时时间。

例如，`RunInstances` 返回实例 ID 后，阿里云 CLI 的调用方可以再执行一次查询，并配置等待器：

```bash
aliyun ecs DescribeInstances \
  --RegionId cn-hangzhou \
  --InstanceIds '["i-bp1234567890example"]' \
  --waiter expr='Instances.Instance[0].Status' to=Running
```

ecctl 创建实例时会自动轮询 `DescribeInstances`，等待实例进入 `Running`，然后再次查询并返回实例信息。
`--timeout` 只调整等待时间上限：

```bash
ecctl ecs instance create \
  --region cn-hangzhou \
  --type ecs.e3.medium \
  --image aliyun_3_x64_20G_alibase_20240528.vhd \
  --sg sg-bp1234567890example \
  --vswitch vsw-bp1234567890example \
  --timeout 10m
```

## 输出与分页归一化

OpenAPI 列表响应经常包含 `Instances.Instance[]` 一类包装层。ecctl 会去掉这些包装层，把资源字段转换为 `snake_case`，并用统一结构返回资源数组和 `pagination`。API 返回可用的总数时，输出中还会包含顶层 `total`。不同 API 仍按各自支持的页码或 Token 方式分页，但 ecctl 的输出格式保持一致。

例如，下面的原始列表命令及其精简响应保留 OpenAPI 的包装层和字段名：

```bash
aliyun ecs DescribeInstances \
  --RegionId cn-hangzhou \
  --MaxResults 2
```

```json
{
  "Instances": {
    "Instance": [
      {"InstanceId": "i-bp1234567890example", "InstanceName": "web-01", "Status": "Running"},
      {"InstanceId": "i-bp0987654321example", "InstanceName": "web-02", "Status": "Running"}
    ]
  },
  "NextToken": "next-page-token",
  "RequestId": "A1B2C3D4-1111-2222-3333-1234567890AB"
}
```

由于 `DescribeInstances` 支持 Token 分页，ecctl 在第一页使用 `--limit`，并在返回资源数组时移除 `Instances.Instance` 包装层：

```bash
ecctl ecs instance list --region cn-hangzhou --limit 2
```

```json
{
  "instances": [
    {"id": "i-bp1234567890example", "name": "web-01", "status": "Running"},
    {"id": "i-bp0987654321example", "name": "web-02", "status": "Running"}
  ],
  "pagination": {
    "limit": 2,
    "returned": 2,
    "has_more": true,
    "next_token": "next-page-token"
  }
}
```

查询下一页时，把 `pagination.next_token` 的值传给 `--next-token`。

变更命令的结果包含 `actions`，其中会列出实际调用过的 API；服务端返回 Request ID 时，也会一并记录。结果中还会带上资源信息或删除结果。完整输出格式见[输出、语言与错误](./output.md)。

例如，下面的命令创建 ECS 实例，并等待实例进入 `Running`：

```bash
ecctl ecs instance create \
  --region cn-hangzhou \
  --type ecs.e3.medium \
  --image aliyun_3_x64_20G_alibase_20240528.vhd \
  --sg sg-bp1234567890example \
  --vswitch vsw-bp1234567890example
```

命令完成后，输出中会包含 `RunInstances` 和 `DescribeInstances` 两次调用：

```json
{
  "actions": [
    {"action_name": "RunInstances", "request_id": "A1B2C3D4-1111-2222-3333-1234567890AB"},
    {"action_name": "DescribeInstances", "request_id": "B2C3D4E5-2222-3333-4444-1234567890AB"}
  ],
  "instance": {"id": "i-bp1234567890example", "status": "Running"}
}
```

## DryRun、幂等与安全默认值

如果底层 OpenAPI 支持服务端 `DryRun`，ecctl 会把校验通过的 `DryRunOperation` 响应作为成功结果返回，例如 `{"dry_run":"passed"}`。这和只打印请求而不发送到服务端的 CLI 预演不同。

如果底层 API 提供幂等参数，命令会统一使用 `--idempotency-key`，未传时可以自动生成。需要强制执行的破坏性操作，仍要显式传入 `--force`。

阿里云 CLI 的全局参数 `--dryrun` 只打印序列化后的请求，不会发送请求。运行结果如下：

```bash
aliyun vpc DeleteVpc \
  --RegionId cn-hangzhou \
  --VpcId vpc-bp1234567890example \
  --dryrun
```

```text
Skip invoke in dry-run mode, request is:
------------------------------------
POST /?...&Action=DeleteVpc&...&VpcId=vpc-bp1234567890example&... HTTPS/1.1
Host: vpc.aliyuncs.com
...

Exit code: 0
```

阿里云 CLI 的 OpenAPI 参数 `--DryRun` 会把校验请求发送到服务端：

```bash
aliyun vpc DeleteVpc \
  --RegionId cn-hangzhou \
  --VpcId vpc-bp1234567890example \
  --DryRun true
```

校验通过时，服务端仍以 `DryRunOperation` 错误返回结果，因此阿里云 CLI 的进程退出码为 1：

```text
ERROR: SDK.ServerError
ErrorCode: DryRunOperation
RequestId: ...
Message: Request validation has been passed with DryRun flag set.
...

Exit code: 1
```

ecctl 的 `--dry-run` 使用服务端校验，并将通过的检查作为成功结果返回：

```bash
ecctl vpc delete vpc-bp1234567890example \
  --region cn-hangzhou \
  --dry-run
```

```json
{
  "actions": [{"action_name": "DeleteVpc", ...}],
  "requested_count": 1,
  "available_count": 1,
  "dry_run": "passed"
}

Exit code: 0
```

创建资源需要幂等时，阿里云 CLI 直接使用 `ClientToken`，ecctl 将它统一为 `--idempotency-key`：

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

强制释放实例在两种方式中都需要显式指定，ecctl 同样要求传入 `--force`：

阿里云 CLI：

```bash
aliyun ecs DeleteInstance \
  --region cn-hangzhou \
  --InstanceId i-bp1234567890example \
  --Force true
```

ecctl:

```bash
ecctl ecs instance delete i-bp1234567890example \
  --region cn-hangzhou \
  --force
```

## 结构化错误与操作建议

ecctl 的成功和错误结果都会以结构化数据写入 `stdout`。错误对象包含固定的错误类别和错误码，并说明是否可重试，还可以给出相关字段和处理建议。`actions` 会保留原始 OpenAPI 错误码、消息和 Request ID。

部分资源命令还会根据错误给出处理建议。ECS 创建实例遇到库存错误时，会给出相应的 `DescribeAvailableResource` 查询命令；删除实例遇到状态错误时，会说明应该使用 `--force`，还是先停止实例。

例如，`RunInstances` 返回指定可用区不支持某个实例规格时，OpenAPI 原始错误如下：

```json
{
  "Code": "InvalidResourceType.NotSupported",
  "Message": "instance type ecs.g6.large not exists in [cn-shanghai-g]",
  "RequestId": "..."
}
```

ecctl 会在 `actions` 中保留原始错误，并补充错误类别、对应资源字段、是否可重试和处理命令：

```json
{
  "error": {
    "kind": "service",
    "code": "CloudAPIError",
    "field": "type",
    "retryable": false,
    "suggested_action": "ecctl call ecs DescribeAvailableResource --region cn-shanghai --ZoneId cn-shanghai-g --DestinationResource InstanceType --InstanceType ecs.g6.large"
  },
  "actions": [
    {
      "action_name": "RunInstances",
      "code": "InvalidResourceType.NotSupported",
      "message": "instance type ecs.g6.large not exists in [cn-shanghai-g]",
      "request_id": "..."
    }
  ]
}
```

## 查看命令说明与调用原始 API

阿里云 CLI 通过以下命令查看原始 API 的参数：

```bash
aliyun ecs RunInstances help
```

输出中会列出 OpenAPI 参数：

```text
Parameters:
  --RegionId String  Required
  ...
  --ImageId String  Optional
  --InstanceType String  Optional
  ...
  --SecurityGroupId String  Optional
  ...
  --VSwitchId String  Optional
```

ecctl 可以查看已经支持的资源命令及其参数：

```bash
ecctl capabilities --output json
ecctl schema ecs.instance.create --brief
```

`capabilities` 的精简结果会列出 ecctl 支持的输出格式、`schema` 和错误返回方式：

```json
{
  "cli": "ecctl",
  "schema_version": 1,
  "output_modes": ["json", "text"],
  "schema": {"supported": true, ...},
  "errors": {"structured": true, "stream": "stdout", ...},
  ...
}
```

使用 `schema` 可以查看单个资源命令：

```json
{
  "command": "ecs.instance.create",
  "kind": "mutation",
  "params": {
    "image": {"type": "string", "required": true},
    "type": {"type": "string", "required": true},
    ...
  },
  "contract": {
    "dry_run": {"supported": true, "flag": "dry-run"},
    "idempotency": {"supported": true, "field": "ClientToken", ...},
    "wait": {"waitable": true, "no_wait_flag": "no-wait", ...}
  }
}
```

`schema` 会列出必填参数、风险、`DryRun`、幂等和等待方式。`--api-param key=value` 只在命令明确支持时可用，用于补充少量请求字段。

如果某个 OpenAPI 操作还没有对应的公开资源命令，可以使用 [`ecctl call`](./openapi-call.md)。它保留原始的操作名和请求结构，不会自动等待资源状态、补充幂等参数或整理响应字段。

例如，阿里云 CLI 查询库存时返回原始 OpenAPI 响应：

```bash
aliyun ecs DescribeAvailableResource \
  --RegionId cn-hangzhou \
  --DestinationResource InstanceType \
  --IoOptimized optimized \
  --InstanceType ecs.e3.medium
```

```json
{
  "RequestId": "...",
  "AvailableZones": {...}
}
```

相同操作通过 `ecctl call` 调用时，原始响应完整保留在 `response` 中，外层会增加产品、操作和区域信息：

```bash
ecctl call ecs DescribeAvailableResource \
  --region cn-hangzhou \
  --DestinationResource InstanceType \
  --IoOptimized optimized \
  --InstanceType ecs.e3.medium
```

```json
{
  "product": "ecs",
  "operation": "DescribeAvailableResource",
  "region": "cn-hangzhou",
  "response": {
    "RequestId": "...",
    "AvailableZones": {...}
  }
}
```

`ecctl call` 只在原始响应外增加一层信息。`response` 内的字段名和结构不会变化，也不会应用资源命令的输出整理、等待和幂等处理。

## 如何选择

如果希望使用统一的资源参数、等待操作完成，并获得整理后的输出，可以使用 ecctl 资源命令。需要调用更多 API，或者精确控制单次 API 请求时，可以使用阿里云 CLI 或 `ecctl call`。应用程序需要自行处理请求构造、重试和运行时集成时，可以直接使用 SDK 或 HTTP 请求。

例如，需要通过一个同步资源动作完成实例创建时，使用 ecctl：

```bash
ecctl ecs instance create \
  --region cn-hangzhou \
  --type ecs.e3.medium \
  --image aliyun_3_x64_20G_alibase_20240528.vhd \
  --sg sg-bp1234567890example \
  --vswitch vsw-bp1234567890example
```

还没有对应资源动作的 API，可以直接使用阿里云 CLI；如果希望复用 ecctl 的账号配置，并让输出带上产品、操作和区域信息，可以使用 `ecctl call`：

阿里云 CLI：

```bash
aliyun ecs DescribeAvailableResource \
  --RegionId cn-hangzhou \
  --DestinationResource InstanceType \
  --IoOptimized optimized \
  --InstanceType ecs.e3.medium
```

ecctl:

```bash
ecctl call ecs DescribeAvailableResource \
  --region cn-hangzhou \
  --DestinationResource InstanceType \
  --IoOptimized optimized \
  --InstanceType ecs.e3.medium
```

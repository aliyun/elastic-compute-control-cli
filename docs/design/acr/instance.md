# acr.instance

资源：容器镜像服务 ACR 企业版实例（只读）

优先级：P2

别名：无（`instance` 已是短名；产品名 `acr` 无需别名）

本文件描述 `ecctl acr instance` 的 interface 级命令设计：每个操作命令对应哪些 ACR API，以及范围边界、兼容性、验证与清理。

## 产品与资源身份

- CLI 产品名 `acr`，`api_product: cr`，API 版本 `2018-12-01`，RPC 风格（官方 API 概览与元数据一致）。
- 接入点为地域化 `cr.<region>.aliyuncs.com`（元数据 `location_service_code: acr`，无全局接入点）。`kind: regional`；地域由接入点决定，`ListInstance`/`GetInstance` 均无 `RegionId` 请求参数。
- `identity.field: id`，前缀 `cri-`；`output_root` one=`instance`、many=`instances`。
- 命令形态：`ecctl acr instance list`、`ecctl acr instance get <cri-id>`。
- `expose_default_resource` 评估结论：不适用。当前加载器要求默认资源 `resource == product` 才能直达产品级命令（参考 ack/sandbox），而本资源 canonical ID 由父项固定为 `acr.instance`，且同产品还有 `acr.namespace`、`acr.repository` 兄弟资源；因此不提供 `ecctl acr list/get` 捷径，不修改共享加载器。
- 元数据依据：固定锁 aliyun-openapi-meta@`77bb12a9a78a6620000924a9af5c5a5235f28c19` 已含 `cr/2018-12-01`（119 操作），与 OpenAPI Explorer `api-docs.json`（119 操作、RPC）逐项一致；本地 `make metadata-prepare` 已离线核验。

## 生命周期与命令面边界（无 create/update/delete）

- `cr/2018-12-01` 的 119 个操作中没有 `CreateInstance`/`DeleteInstance`（本地元数据与 Explorer 双重核对）。
- 企业版实例通过 BSS 售卖 [CreateInstance](https://help.aliyun.com/zh/acr/developer-reference/createinstance-1) 购买（`ProductCode=acr`、`SubscriptionType=Subscription` 预付费、自动支付、`Period` 按月），属付费售卖 API 且不在本 API 版本，故不实现 create/delete。
- 本版本没有实例属性修改 API，不实现 update；`ChangeResourceGroup` 属资源组治理，本期不纳入（见下文）。
- 个人版实例使用 `2016-06-07` ROA API，不在固定元数据锁内，不在本期范围。`ListInstance`/`GetInstance` 是企业版实例契约：`InstanceSpecification` 仅 `Enterprise_Basic`/`Enterprise_Standard`/`Enterprise_Advanced` 枚举，ListInstance 官方文档称该字段为「企业版规格」，父项只读实测亦仅返回企业版实例。

## `ecctl acr instance get <cri-id>`

调用 API：

- [GetInstance](https://help.aliyun.com/zh/acr/developer-reference/api-cr-2018-12-01-getinstance)：查询单个企业版实例信息。

契约：

- 请求参数仅 `InstanceId`（query，string，必填），由位置参数 `<cri-id>` 提供。
- RAM 权限点 `cr:GetInstance`（get），资源 `acs:cr:{#regionId}:{#accountId}:instance/{#InstanceId}`；只读，无计费影响。
- 响应字段映射（snake_case 输出，空白字段按规则省略）：

| 输出字段 | API 字段 | 说明 |
| --- | --- | --- |
| `id` | `InstanceId` | `cri-` 前缀 |
| `name` | `InstanceName` | 实例名 |
| `status` | `InstanceStatus` | 原值透传，见下 |
| `spec` | `InstanceSpecification` | `Enterprise_Basic`/`Enterprise_Standard`/`Enterprise_Advanced` |
| `resource_group` | `ResourceGroupId` | 资源组 ID |
| `tags` | `Tags` | `TagKey/TagValue` 数组经现有全局 tags 字段映射器归一为 map |
| `create_time` | `CreateTime` | UNIX 毫秒时间戳，原值透传（本 API 为 integer） |
| `modified_time` | `ModifiedTime` | UNIX 毫秒时间戳，原值透传（本 API 为 integer） |
| `instance_issue` | `InstanceIssue` | 实例问题（如 `OSS_TOO_MANY_BUCKETS`），空则省略 |

注意事项：

- `status` 原值透传大写枚举：`PENDING`/`INIT_ERROR`/`STARTING`/`RUNNING`/`STOPPING`/`STOPPED`/`DELETING`/`DELETED`；get 不等待、不过滤状态。状态枚举覆盖 `PENDING`，初始化中的实例按文档契约正常返回记录；E2E 断言只依赖 RUNNING 前置实例，不依赖 PENDING 行为。
- `GetInstance` 响应不含 `RegionId`（与 `ListInstance` 不同），get 输出不含 `region` 字段。
- 错误默认透传：文档列出 500 `SystemError`、403 `NoPrivilege` 等，原始 code/message/request_id 保留在 `actions[]`。显式请求 ID 且实例不存在时走引擎现有通用 NotFound 归类（exit 4），不新增 ACR 专属 hook，也不新增转译登记。
- 默认路径只调用 `GetInstance`；`--with-usage`/`--with-endpoint` 本期不实现（对应 API 见下文）。

## `ecctl acr instance list`

调用 API：

- [ListInstance](https://help.aliyun.com/zh/acr/developer-reference/api-cr-2018-12-01-listinstance)：查询企业版实例列表。

契约：

- 请求参数全部可选：`PageNo`（int）、`PageSize`（int）、`InstanceStatus`（string 枚举）、`ResourceGroupId`（string）、`InstanceName`（string，官方已标 deprecated）。
- 仅 page 分页、无 token 分页 → 暴露 `--page`/`--limit`，不暴露 `--next-token`。`--limit` 默认 100：官方文档与元数据均未声明服务端最大值（文档默认 30 仅为缺省值）；若 live 实测发现服务端上限，按观察证据调整 spec `max` 并重新验证。`--page` 默认 1。
- 筛选全部走 `--filter`：`status` → `InstanceStatus`（枚举同上），`resource-group` → `ResourceGroupId`。`InstanceName` 已废弃，不建模 name 过滤；确有需要时用 `--api-param InstanceName=xxx` 逃生口（list/get 均开放 `api_param` control）。
- RAM 权限点 `cr:ListInstance`（list），资源 `acs:cr:{#regionId}:{#accountId}:instance/*`；只读，无计费影响。
- 响应映射：`Instances[]` → `instances[]`；`TotalCount` 为文档明确的总数 → 顶层 `total`；分页输出 `pagination.page`，有下一页时 `pagination.next_page`。
- 列表项字段：`id`/`name`/`status`/`spec`/`region`（`RegionId`）/`resource_group`/`tags`/`create_time`/`modified_time`/`instance_issue`。`CreateTime`/`ModifiedTime` 在本 API 为字符串型毫秒时间戳，原值透传，不做隐式类型转换。
- 空列表是合法结果：该地域没有企业版实例时返回空数组（父项实测多个地域 `TotalCount=0`）。

## 暂不进入主命令面的 API

- `GetInstanceUsage`（配额使用情况）、`GetInstanceEndpoint`/`ListInstanceEndpoint`/`GetInstanceVpcEndpoint`（访问入口）：可作为后续 `get --with-usage`/`--with-endpoint` 扩展，本期默认不展开。
- `GetInstanceCount`、`ListInstanceRegion`（支持地域）：统计/元数据查询，本期不纳入。
- `ChangeResourceGroup`：资源组归属变更，后续可评估为 `update --resource-group`，本期只读范围不纳入。
- 实例接入点 ACL、自定义域名、VPC 联动等写 API：不在本期范围。
- 废弃参数：`ListInstance.InstanceName`（不建模过滤）。

## 兼容性

- 纯新增：新建 `specs/acr/product.yaml` 与 `specs/acr/instance.yaml`，不改动任何现有产品行为；无新增 Go hook（tags 归一复用现有全局字段映射器）。
- 共享面：`specs/acr/product.yaml` 的 resources 列表、`pkg/spec/catalog_generated.go`（`make generate`）、`e2e/coverage.yaml` 注册表。兄弟子项合并顺序 instance→namespace→repository；本子项从最新 `origin/main` 实施，避免重复修改已交付子项的共享文件。
- 无破坏性变更；schema 自省新增 `acr.instance`（list/get）。

## 验证与清理

离线验证：

- `make metadata-prepare`、`make generate`（提交生成物）、`make test`、`make lint`；spec 变更通过 drift 检查。
- 单测覆盖：list/get 请求映射（page/limit/filter）、响应字段提取、空列表、NotFound 归类与错误透传。

live E2E（只读，零新增付费资源、零预算消耗）：

- 先跑 `make -C e2e lint`；public 用例 runner 参数 `--ecctl-bin e2e/bin/ecctl-public --surface public`。
- 用例 `e2e/cases/acr/instance-lifecycle.yaml`，`requires_prerequisites: [acr.instance]`，步骤全部只读：
  1. `list`：断言 `instances` 数组与 `pagination`，cn-beijing 期望 `total >= 1`；
  2. `list --limit 1`：断言 page 分页 `has_more`/`next_page` 映射（cn-beijing 有 2 个实例）；
  3. `list --filter status=RUNNING`：断言包含前置实例；
  4. `get cri-egnl7x14p5vw7qiu`：断言 `id` 相等、`spec` 属 `Enterprise_*`、`status=RUNNING`。
- 前置声明：在忽略的 `e2e.local.yaml` 增加 cn-beijing 地域 profile，`prerequisites` 声明 `acr.instance`（mode=lookup，`instance_id: cri-egnl7x14p5vw7qiu`，RUNNING/Enterprise_Basic，名称 test12333，资源组 rg-acfm2xwmxvrzq6q）。不 teardown；**严禁删除或变配该共享实例**。另一 cn-beijing 实例 `cri-idtyr9l0byb4a7se` 长期 PENDING，不作为断言对象。
- 执行端：本机 ARUN，身份 `ecctl-e2e-arun`（External credential_process，`ECCTL_ALIYUN_CONFIG_PATH=/etc/ecctl-harness/e2e-aliyun.json`，pin AccountId `1754580903499898` / RAMUser `229050894712721687`）。
- 清理：无（只读用例不创建资源，无清理步骤，清理日志为空）。
- 覆盖注册：`e2e/coverage.yaml` 登记 `acr.instance` list/get，live 通过后逐项 `live-pass`。

## 待核实契约的结论

- ListInstance 是否含个人版实例：不含。文档字段语义（「企业版规格」）、`Enterprise_*` 专属枚举与父项只读实测一致；个人版走 2016-06-07 ROA，不在固定元数据。
- GetInstance 对 PENDING 实例的返回：状态枚举包含 `PENDING`，get 按契约透传状态、不做状态门禁；E2E 不依赖该行为。
- `--with-usage`/`--with-endpoint`：本期不纳入，默认 get 仅 `GetInstance`；对应 API 已登记在「暂不进入主命令面的 API」。

## 来源

- [ListInstance - 查询实例列表](https://help.aliyun.com/zh/acr/developer-reference/api-cr-2018-12-01-listinstance)
- [GetInstance - 查询实例信息](https://help.aliyun.com/zh/acr/developer-reference/api-cr-2018-12-01-getinstance)
- [CreateInstance（BSS 售卖）](https://help.aliyun.com/zh/acr/developer-reference/createinstance-1)
- [API 概览（cr/2018-12-01，RPC）](https://help.aliyun.com/zh/acr/developer-reference/api-cr-2018-12-01-overview)
- [OpenAPI Explorer cr/2018-12-01 api-docs.json](https://api.aliyun.com/meta/v1/products/cr/versions/2018-12-01/api-docs.json)
- 固定元数据锁 aliyun/aliyun-openapi-meta@`77bb12a9a78a6620000924a9af5c5a5235f28c19`（本地离线核验）。
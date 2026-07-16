# ecctl CLI 设计规则

本文档是 `ecctl` CLI 的纲领，只记录稳定规则和最小示例。实现细节见 [resource-spec.md](resource-spec.md)。

## 1. 总原则

- Agent-first：命令表达资源意图，不要求调用方记 OpenAPI 细节。
- Spec-first：资源行为优先写在 `specs/**/*.yaml`，Go hook 只处理 spec 难以表达的派生、归一化和特殊编码。
- 只做输入、输出的转译：统一 verb、参数和输出形态；不隐式改变云 API 行为。
- 小命令面：高频资源用统一 CRUD；长尾能力不进入主命令面。
- 可解析输出：stdout 输出结构化结果；stderr 不承载可解析错误信息。
- 同步默认：异步 mutation 默认等到目标状态并回读资源。
- 原始错误默认透传：OpenAPI code/message/request_id 进入 `actions[]`。
- 特殊转译必须人工 review，并登记在本文档的 Exception 转译或 API 转译中。

## 2. 命令形态

标准语法：

```bash
ecctl <product> <resource> <action> [id] [flags]
ecctl <product> <parent> <resource> <action> [id] [flags]
```

示例：

```bash
ecctl ecs instance list --filter status=running
ecctl ecs instance get i-xxx
ecctl ecs instance create --type ecs.e3.medium --image alinux3 --sg sg-xxx --vswitch vsw-xxx
ecctl vpc vsw list --filter vpc=vpc-xxx
```

规则：

- 基础 action 固定为 `list/get/create/update/delete`。
- 资源可以有一层父资源。父资源是规范路径的一部分，命令、schema ID 和文档索引都必须保留完整的 `<product>.<parent>.<resource>` 路径；不得把子资源压平为 `<product>.<resource>`。
- 资源专属 action 仍挂在资源下，例如 `ecctl ecs sg authorize`。
- 产品内可有短别名，例如 `vpc vsw`；不做跨产品全局资源命令，例如 `ecctl sg ...`。只有vpc例外，由于高频使用，vpc即是资源、也是产品。
- 长资源名应该设置产品内别名，优先使用资源 ID 前缀，例如 `ecs auto-provisioning-group` 的别名是 `ecs apg`；没有稳定 ID 前缀时使用资源名首字母缩写。别名只作为输入捷径，schema、自省和文档索引仍使用 canonical 资源名。
- 业务产品内的标签和资源组归属是资源关系，不设计 `ecs tag` / `ecs resource-group` 这类产品内伪资源命令；在支持该能力的真实资源上表达，例如 `ecs instance update --tag k=v`、`ecs instance update --resource-group rg-xxx`。变更类 API 归入对应资源的 `update`，查询通过对应资源的 `get/list` 或过滤参数表达。跨产品批量治理能力由和 `ecs` 平级的 `tag`、`rg` 产品承载。

Action 词表：

| Action | 使用场景 | 规则 |
| --- | --- | --- |
| `list` | 查询资源集合 | 必须支持分页；所有筛选条件必须放在 `--filter key=value` 中，不为筛选条件设计独立 flag。 |
| `get` | 查询单个资源详情 | 默认调用主详情 API；额外详情用 `--with-*` 显式开启，例如 `--with-auto-renew`、`--with-results`。 |
| `create` | 创建资源 | 创建类异步 API 默认等待目标资源可见并回读资源视图。导入已有对象但语义是“创建本产品资源”时仍归入 `create`，例如 keypair import。 |
| `update` | 修改资源属性、关系、策略、规格、计费、开关 | 默认承载修改类 API；`lock/unlock`、`apply/cancel association` 等关系变化优先用 `update --lock`、`--unlock`、`--attach-*`、`--detach-*`，不默认扩展成顶层 action。 |
| `delete` | 删除资源 | 必须保持 `--force=false` 安全默认；删除多个资源时仍是同一 action。 |
| `enable` / `disable` | 功能或能力开关 | 用于开通/关闭某项功能或能力，例如 `notification enable`、`associated-transfer disable`。运行态生命周期用 `start/stop`，不用 `enable/disable`。 |
| `start` / `stop` / `reboot` | 运行态生命周期 | 用于实例、执行流、流水线等”运行/停止/重启”语义；单个和批量 API 由 ID 数量决定，不拆 `start-one` / `start-batch`。不要用 `enable/disable` 表达运行态。 |
| `install` | 安装 agent 或服务能力 | 只在用户语义就是安装时使用，例如 `assistant install`。 |
| `invoke` / `exec` / `sendfile` | 远程执行或下发 | 异步执行默认等待并回读执行结果；结果查询不单独扩展成 `result list`。 |
| `apply` / `remove` | 批量绑定/解绑 | 用于跨产品批量操作，例如 `tag resource apply` 批量打标签、`tag resource remove` 批量解标签。单个业务资源的标签关系仍优先放在对应资源的 `update` 中。 |
| `authorize` / `revoke` | 授权规则增删 | 入/出方向等规则维度用 flag 控制，例如 `--direction ingress|egress`；不设计 `authorize-egress`、`revoke-egress`。 |
| `attach` / `detach` | 资源间绑定/解绑 | 仅当绑定/解绑是一等常用工作流时使用，例如 `disk attach` / `disk detach`。如果只是属性集合或关系集合的期望状态，归入资源 `update`。 |
| `copy` / `clone` / `export` / `import` | 数据或镜像搬迁 | 语义是复制已有资源时用 `copy` / `clone`；导出到外部系统用 `export`；从外部系统导入用 `import`。取消同一复制任务可用原 action 的 `--cancel`，独立执行任务才用 `cancel`。 |
| `renew` | 续费 | 只用于计费资源续费，不承载询价；询价不进入主命令面。 |
| `monitor` | 监控数据查询 | 用于时间序列监控，不和资源详情 `get` 混在一起。 |
| `reset` / `reinit` / `redeploy` | 恢复、初始化、重新部署 | 这些是有明显副作用的运维动作，保留为资源专属 action。 |
| `upgrade` | 版本升级 | 用于有完整任务生命周期（默认等待 + 可暂停/恢复/取消）的版本升级，例如 ACK `cluster upgrade` / `nodepool upgrade`。版本切换若没有任务模型、只是属性变更，应继续走 `update --target-version` / `update --create-version`（参考 `ecs launch-template`）。 |
| `pause` / `resume` | 暂停或恢复任务 | 只用于有明确任务生命周期的长时任务，例如 ACK 升级任务；不能用来表达普通资源的启停。 |
| `repair` | 修复 / 自愈 | 用于触发资源的修复或自愈，例如 ACK `nodepool repair`。与 `reset`（回初始状态）/`reinit`（重新初始化）的区别：`repair` 修的是异常态，不改变期望状态。修复维度用 flag 表达，例如 `--vulnerabilities` 触发节点池 CVE 修复，不拆 `fix-vuls`。 |
| `cancel` / `end` | 取消任务或结束会话 | `cancel` 用于任务/执行对象的取消；`end` 用于会话结束，例如 terminal session。 |

命令面收敛规则：

- 同一个用户动作因为方向、类型、模式、版本等差异调用不同 API 时，用 flag 选择 API，不拆 action。例如 `sg authorize --direction egress`、`launch-template update --create-version`、`event list --view disk-full-status`。
- 一个命令映射多个 API 时，设计文档和 spec 必须写出触发每个 API 的 flag；不能只写“指定某场景时调用”。
- `get` 的默认路径必须是主详情 API；附加关系、执行结果、自动续费、维护属性、支持规格等用 `--with-*`。
- 关系型变更优先表达“期望状态”：实例的 RAM role、keypair、安全组、标签、资源组归属等归入 `instance update`，由实现计算 attach/detach 或 tag/untag 差异。
- 方向型规则不进入 action 名：不用 `authorize-egress`、`revoke-egress`、`rule update`、`egress-rule update`，而是 `authorize/revoke/update` 加 flag。
- 能力域可以有子对象命令，但只用于真实对象类别，不用于 API 细节。例如 `diagnostic metric-set list` 和 `diagnostic report list` 可以存在；`diagnostic report-result list` 这类结果查询不应为了 API 单独成命令。
- 废弃、不推荐、询价、ClassicLink、故障反馈等低频能力不进入主命令面；在资源设计文档的“暂不进入主命令面的 API”或“废弃/不推荐 API”说明。
- 资源设计文档的章节顺序固定为：`create`、`update`、`delete`、`get`、`list`，然后是资源专属 action。

## 3. 命名边界

| 层面 | 规则 | 示例 |
| --- | --- | --- |
| spec / Go 字段 | snake_case | `dry_run`, `security_group_ids` |
| CLI flag | kebab-case | `--dry-run`, `--security-group-ids` |
| stdout JSON | snake_case | `dry_run`, `request_id`, `has_more` |
| OpenAPI 字段 | 保持云厂商命名 | `InstanceIds`, `RequestId` |

转换只发生在 CLI flag 层：

```yaml
# specs/ecs/instance.yaml 片段
dry_run:
  type: boolean
```

```bash
ecctl ecs instance create ... --dry-run
```

对应 stdout：

```json
{"dry_run": "passed"}
```

## 4. 参数规则

- 当前命令目标用位置参数：`ecctl ecs instance get i-xxx`。
- 一等云资源引用用资源名，不让 flag 名以 `-id` 结尾：`--vpc`、`--vswitch`、`--sg`。
- 内部子对象 ID 的 flag 名以 `-id` 结尾：`--rule-id sgr-xxx`。
- 多 ID 用位置参数或 `--ids`，不接受 JSON 数组。
- `--tag key=value` 是赋值；`--filter tag.env=prod` 是查询过滤。
- `--api-param key=value` 是逃生口，只能用于 spec 明确允许的 operation。
- delete 的 `--force` 默认必须是 `false`；不能为了清理便利默认强删，也不能隐式解绑或删除依赖资源。
- list 的 `--limit` 默认是 `100`；如果底层 API 最大值小于 100，使用 API 最大值，不继承 Describe API 常见的 10 条默认值。
- list 分页方式必须跟随底层 list API，并按能力确定优先级：只要 API 支持 `NextToken/MaxResults` 或同类 token 分页，即使同时支持 `PageNumber/PageSize`，也必须优先暴露 `--next-token/--limit`；只有 API 不支持 token 分页时，才暴露 `--page/--limit`。同一个 list 命令不能同时暴露 `--page` 和 `--next-token`，输出的 `pagination` 也不能混用 `page/next_page` 与 `next_token`。
- list 的 `--next-token` 必须放在参数分类中，位于 `--limit` 的后面。在 spec 的 operation controls 中，`next_token` 紧跟 `limit`。
- list 的所有筛选条件必须通过 `--filter key=value` 传递，不为筛选条件设计独立 flag。以下类型的参数不属于筛选条件，仍作为独立 flag：
  - positional 参数（如资源 ID）。
  - 模式开关（决定调用哪个 API 或改变输出结构的 flag），例如 `node list --free / --hyper`、`command list --invocations`。
  - 响应格式控制，例如 `--accept-language`、`--verbose`、`--content-encoding`、`--include-output`。

结构化入参按复杂度分层，CLI 不为复杂对象内部字段设计二级 flag：

| OpenAPI 展开形态 | 语义 | CLI 推荐表达 |
| --- | --- | --- |
| `A.k1=v1` | 单对象的标量字段 | `--a k1=v1,k2=v2` |
| `A.k1.1=v1` | 单对象里的标量数组 | `--a-k1 v1 --a-k1 v2` |
| `A.1=v1` | 标量数组 | `--a v1 --a v2` |
| `A.1.k1=v1` | 对象数组，item 字段是标量 | 自然单数 flag，例如 `--data-disk k1=v1,k2=v2`；无自然单数时用 `--a-item` |
| `A.1.k1.k2=v1` | 对象数组，item 内还有对象或数组 | 自然单数 flag 传 JSON / `@file.json`，例如 `--container @app.container.json` |

- inline flag 只承接浅层可内联结构：标量、标量数组、标量字段对象、标量字段对象数组。
- 超过浅层边界的复杂对象用 JSON 字符串或 `@file.json`，例如 ECI `container` / `init_container`。
- 复杂对象数组仍使用自然单数 flag，每个 flag 传一个 item：`--container @app.container.json --container @sidecar.container.json`。
- JSON / `@file.json` 内容使用 spec 字段名，也就是 snake_case；CLI flag 和 inline key 使用 kebab-case。
- help 和 schema 必须声明每个复杂参数的输入形态、inline 字段、item flag 和 `@file` schema；Agent 不从 OpenAPI 展开名或自然语言描述推导传参方式。

子集合增删使用 `+-` 前缀语法：`update` 中对子集合（IP、CIDR、路由条目、授权规则等）的增删，不拆成 `--add-*` / `--remove-*` 两个 flag，而是用同一个 flag 加 `+` / `-` 值前缀区分方向。`+` 表示添加，`-` 表示删除，可重复传入或在同一次调用中混用。

示例：

```bash
ecctl ecs instance list i-1 i-2
ecctl ecs instance list --ids i-1,i-2
ecctl ecs instance create --tag env=prod
ecctl ecs instance list --filter tag.env=prod
ecctl ecs instance create --system-disk category=cloud_essd,size=40 --data-disk size=100,category=cloud_essd
ecctl eci cg create --container @app.container.json
ecctl eci cg create --container '{"name":"app","image":"nginx"}'
ecctl lingjun vpd update vpd-xxx --cidr +10.0.0.0/8 --cidr -172.16.0.0/12
ecctl lingjun lni update lni-xxx --ip +192.168.1.10 --ip -192.168.1.20
```

## 5. 输出规则

- 默认 JSON；`--output text` 仅作为人类可读视图；不引入 table 模式。
- 去掉 OpenAPI 包装层：`Instances.Instance[]` -> `instances[]`。
- 输出字段保持 snake_case，并优先提供 `id/status/name` 等稳定字段。
- 资源视图中的空白字段可以省略：`""`、`{}`、`[]`、`0` 不必返回；有编排语义的协议字段除外，例如 API 明确定义的 `total`、`pagination.returned`。
- list 输出资源数组和 `pagination`；只有当前分页 API 返回有意义的总数时才输出顶层 `total`。token 分页 API 未返回总数或声明总数无意义时，必须省略 `total`，不能用本页数量伪造总数。
- page 分页输出 `pagination.page`，有下一页时输出 `pagination.next_page`；token 分页输出 `pagination.next_token`，不输出 `page` 或 `next_page`。
- mutation 输出 `actions[]` 和最新资源视图；不输出顶层 `request_id`。

`ecctl ecs instance list --filter status=running` 的 stdout 示例：

```json
{
  "instances": [{"id": "i-xxx", "status": "running"}],
  "pagination": {"limit": 100, "returned": 1, "has_more": false}
}
```

`ecctl ecs instance create ...` 成功后的 stdout 示例：

```json
{
  "actions": [{"action_name": "RunInstances", "request_id": "req-xxx"}],
  "instance": {"id": "i-xxx", "status": "running"}
}
```

## 6. 错误、退出码和状态

错误写 stdout JSON。原始 OpenAPI 错误默认不翻译，放入 `actions[]`；少数跨资源语义可以归一，例如 NotFound。

| Code | 含义 |
| --- | --- |
| 0 | 成功 |
| 1 | 客户端错误 |
| 2 | 云服务错误 |
| 3 | 等待或隐藏状态重试超时 |
| 4 | 资源不存在 |

`ecctl ecs instance create ... --dry-run` 预检通过时必须 exit 0，stdout 示例：

```json
{"dry_run": "passed", "requested_count": 1, "available_count": 1}
```

`ecctl ecs instance delete i-xxx` 遇到云 API 失败时保留原始动作信息，stdout 示例：

```json
{
  "actions": [{"action_name": "DeleteInstance", "request_id": "req-xxx", "code": "IncorrectInstanceStatus.Initializing"}],
  "error": {"code": "CloudAPIError", "message": "API call failed; see actions for details", "retryable": false}
}
```

## 7. 转译登记

Exception 转译：

- 默认不转译 OpenAPI exception；`actions[].code`、`actions[].message`、`actions[].request_id` 保留原始信息。
- NotFound：单资源 get/delete 明确请求 ID，或 mutation 明确引用一等资源 ID 且云 API 返回不存在时，顶层 `error.code` 可归一为 `NotFound`，exit code 为 4；原始 code/message 仍保留在 `actions[]`。
- DependencyViolation：云 API 明确返回依赖冲突 / 资源占用类错误时，顶层 `error.code` 可归一为 `DependencyViolation`，exit code 为 2；原始 code/message 仍保留在 `actions[]`。

API 转译：

- DryRun：不改变云 API dry-run 行为，只把 CLI 结果归一为可编排输出；预检通过 exit 0，stdout 返回 `dry_run: passed`。
- List pagination：list 命令显式发送底层 API 支持的一种分页参数。API 支持 token 分页时优先使用 `limit/next_token` 并映射到 `MaxResults/NextToken` 或同类参数；只有不支持 token 分页时才使用 `limit/page` 并映射到 `PageSize/PageNumber`。默认 limit 为 100 或 API 最大值，避免继承 Describe API 的 10 条隐式默认。
- ECS image name：`ecs instance create --image <name>` 可通过 DescribeImages 派生为 `ImageId`；显式 `.vhd` / 镜像 ID 不做额外查询。
- lingjun vcc delete: 灵骏 VCC 没有独立 DeleteVcc 接口，CLI delete 实际调用 RefundVcc。两者语义一致（释放 VCC 资源），CLI 层保持 delete 形态；底层 OpenAPI 调用 RefundVcc。如果 eflo 后续上线真正的 DeleteVcc，把 spec 的 delete_to_absent binding api 字段改回 DeleteVcc 即可。
- lingjun vcc update --bandwidth: UpdateVcc 在 OpenAPI 元数据中 OrderId 标记为可选，但 PAI 商业流程要求带宽变更必须先购买变配订单。CLI 层通过 require_when 强制 --bandwidth ⇒ --order-id，缺一不可，避免静默失败；如上游放宽限制可移除该约束。

新增或修改本节条目必须人工 review。

## 8. 新命令检查清单

- 是否符合 `ecctl <product> <resource> <action>`。
- 是否先改 spec，而不是在调用点硬编码 OpenAPI 参数。
- CLI flag 是否由 snake_case 转成 kebab-case。
- stdout 是否保持 snake_case，并避免泄漏 PascalCase 包装层。
- 空白输出字段是否按规则省略，且有编排语义的 0 值是否保留。
- mutation 是否返回 `actions[]` 和可继续编排的资源状态。
- delete 是否保持 `--force=false` 安全默认。
- list 是否默认返回 100 条或 API 最大值。
- list API 同时支持 token 和 page 分页时，是否优先选择了 `--next-token/--limit`。
- list 的筛选条件是否全部通过 `--filter` 而非独立 flag 传递。
- list 使用 token 分页时，`--next-token` 是否在参数分类中紧跟 `--limit`。
- 多 API 命令是否写明触发 API 的 flag，而不是拆成 `*-egress`、`*-status`、`*-result` 等 API 形态命令。
- 资源关系、开关、方向、版本、结果查询是否优先收敛到 `update/get/list` 的 flag。
- 新 action 是否在 Action 词表中；如果不在，是否确实是用户一等动作而不是 OpenAPI 名称泄漏。
- 错误是否结构化，退出码是否可脚本分支。
- Exception/API 转译是否已登记并经过人工 review。
- 用户可见文案是否放在 `pkg/i18n` 或 spec localized text 中。

# Resource Spec 语法

本文说明当前实现支持的 `resource spec` YAML 语法。资源规格用于生成高层资源命令，并驱动 executor 调用云 API、执行状态探针、等待资源状态和归一化输出。

## 1. 文件位置

默认资源规格位于：

```text
specs/<product>/<resource>.yaml
```

例如：

```text
specs/ecs/instance.yaml
specs/ecs/sg.yaml
specs/vpc/vpc.yaml
specs/vpc/vswitch.yaml
```

产品规格位于：

```text
specs/<product>/product.yaml
```

运行时可以通过 `ECCTL_SPEC_DIR` 指定外部规格目录。外部目录保持同样的 `<product>/product.yaml` 与 `<product>/<resource>.yaml` 结构。

## 2. 产品字段

`product.yaml` 描述产品命令本身：

```yaml
schema_version: 1
product: vpc
description:
  en: Manage VPC resources
  zh-CN: 管理 VPC 资源
examples:
  - ecctl vpc list
  - ecctl vpc create --name prod-vpc --cidr 10.0.0.0/16
```

产品帮助页从这里读取产品说明和关键示例。资源 YAML 不应重复产品说明。

## 3. 资源顶层字段

```yaml
schema_version: 2
product: ecs
resource: instance
kind: regional
aliases: [vm]
display_name:
  en: Instance
  zh-CN: 实例
description:
  en: Manage instance resources
  zh-CN: 管理实例资源
examples:
  - ecctl ecs instance list
  - ecctl ecs instance get <instance-id>
messages: {}
identity: {}
schema:
  fields: {}
controls: {}
probes: {}
waiters: {}
bindings: {}
operations: {}
```

必填字段：

| 字段 | 含义 |
| --- | --- |
| `schema_version` | 当前资源规格为 `2` |
| `product` | 产品名，也是一级命令名，例如 `ecs`、`vpc` |
| `resource` | 资源名；如果等于 `product`，直接生成产品命令 |
| `kind` | 资源类型；当前区域资源使用 `regional` |
| `schema.fields` | 资源输入字段的唯一类型和描述来源 |

可选字段：

| 字段 | 含义 |
| --- | --- |
| `api_product` | OpenAPI 产品代码；为空时使用 `product`。用于 CLI 产品名和云产品代码不一致的治理产品，例如 `rg` -> `ResourceManager`。 |
| `aliases` | 资源子命令别名 |
| `display_name` | 展示名称 |
| `description` | 资源命令说明 |
| `examples` | 资源命令帮助中的关键示例，建议 2-4 条 |
| `messages` | 规格私有的本地化消息 |
| `identity` | 资源 ID 与输出根节点配置 |
| `controls` | 非资源模型的命令控制项，例如分页、等待、兜底参数 |
| `probes` | 查询 API 与响应字段提取规则 |
| `waiters` | 基于 probe 的等待规则 |
| `bindings` | 写 API 调用和请求映射 |
| `operations` | CLI operation 定义 |

旧顶层 `params`、`actions`、`transitions`、`transforms` 不再是合法资源规格语法。

本地化文本可以写成字符串，也可以写成语言映射：

```yaml
description: Manage VPC resources

description:
  en: Manage VPC resources
  zh-CN: 管理 VPC 资源
```

## 4. Identity

`identity` 描述资源 ID 字段和输出根节点：

```yaml
identity:
  field: id
  prefix: i-
  output_root:
    one: instance
    many: instances
```

| 字段 | 含义 |
| --- | --- |
| `field` | 资源 schema 中代表 ID 的字段名 |
| `prefix` | 资源 ID 前缀 |
| `output_root.one` | 单资源输出根节点 |
| `output_root.many` | 多资源输出根节点 |

`identity` 是 CLI/output 元数据，不会出现在 `ecctl schema <command>` 的参数 schema 中。

## 5. Schema

`schema.fields` 是资源字段的唯一建模位置。operation 只引用这些字段，不重复声明类型。

支持的字段类型：

| `type` | CLI 解析 |
| --- | --- |
| `string` | 字符串 flag |
| `cidr` | 字符串 flag |
| `boolean` | bool flag |
| `integer` | int flag |
| `number` | float flag |
| `duration` | Go duration，例如 `300s`、`5m` |
| `key_value` | `key=value` 字符串；`repeatable: true` 时可重复 |
| `string_array` | 逗号分隔的字符串数组 flag，例如 `--tag-keys env,owner` |
| `array` | JSON 数组 flag |
| `object` | JSON 对象 flag |

`array` 必须声明 `items`。`object` 可以声明 `fields`，子字段结构和父字段一致，都使用 `type`、`description`、`default`、`enum`、`items`、`fields` 等资源语义属性。

```yaml
schema:
  fields:
    image:
      type: string
      description:
        en: ECS image ID or name
        zh-CN: ECS 镜像 ID 或名称
    data_disks:
      type: array
      description:
        en: Data disks to attach when creating the instance.
        zh-CN: 创建实例时挂载的数据盘列表。
      items:
        type: object
        fields:
          category:
            type: string
            description:
              en: Data disk category.
              zh-CN: 数据盘类型。
          auto_snapshot_policy:
            type: string
            description:
              en: Automatic snapshot policy ID for the data disk.
              zh-CN: 数据盘使用的自动快照策略 ID。
          size:
            type: integer
            description:
              en: Data disk size in GiB.
              zh-CN: 数据盘容量，单位 GiB。
    system_disk:
      type: object
      description:
        en: System disk configuration.
        zh-CN: 系统盘配置。
      fields:
        category:
          type: string
          default: cloud_essd
          description:
            en: System disk category.
            zh-CN: 系统盘类型。
```

资源 schema 是面向用户的资源模型，不展示底层 API 字段名。

## 6. Controls

`controls` 描述非资源模型的命令控制项。operation 必须显式选择要暴露哪些 control。整数 control 可以声明 `max`，CLI 会把上限显示在对应参数的 help 中。

```yaml
controls:
  filter:
    type: key_value
    repeatable: true
    description:
      en: Filter expression key=value.
      zh-CN: 过滤表达式 key=value。
  limit:
    type: integer
    default: 100
    max: 100
    description:
      en: Maximum resources to return.
      zh-CN: 最多返回资源数量。
  page:
    type: integer
    default: 1
    description:
      en: Results page to return.
      zh-CN: 返回结果页码。
  next_token:
    type: string
    description:
      en: Next page token.
      zh-CN: 下一页查询 Token。
  dry_run:
    type: boolean
    description:
      en: Validate without applying changes.
      zh-CN: 只校验不执行。
  no_wait:
    type: boolean
    description:
      en: Return before waiter completion.
      zh-CN: 等待完成前返回。
  timeout:
    type: duration
    default: 300s
    description:
      en: Wait timeout.
      zh-CN: 等待超时时间。
  api_param:
    type: key_value
    repeatable: true
    description:
      en: Additional request parameter key=value.
      zh-CN: 额外请求参数 key=value。
```

list 分页 controls 必须根据 API 能力二选一。只要 API 支持 token 分页，即使同时保留 page 参数，spec 也必须选择 `limit` 和 `next_token`，并把它们映射到 `MaxResults/NextToken` 或同类参数；只有 API 不支持 token 分页时，才选择 `limit` 和 `page`。非 list operation 中的附加分页 probe 也必须遵循相同的 API 参数优先级。token 必须作为不透明字符串原样传递，不能解析、改写或自行生成。

`api_param` 是兜底 control，不是资源字段。CLI help 会把它放在 operation 参数分组最后。

## 7. Operations

`operations` 定义 CLI operation。它选择有序输入字段和 control，并通过 `call` 或 `workflow` 连接执行逻辑。

```yaml
operations:
  create:
    description:
      en: Create instance
      zh-CN: 创建实例
    input:
      fields:
        - type:
            required: true
        - image:
            required: true
        - sg:
            required: true
        - vswitch:
            required: true
        - data_disks
        - system_disk
      controls:
        - dry_run
        - no_wait
        - timeout
        - api_param
    workflow:
      - binding: create_to_running
        wait_unless: input.no_wait
      - probe: state
        ids:
          - $context.id
        unless: input.no_wait
      - emit:
          fields:
            id: $context.id
            image: $input.image
            type: $input.type
```

workflow step 可以用 `when` / `unless` 按输入条件选择 API，用 `when_any` 声明触发该 step 的输入字段。多 API 命令必须在 spec 中写出触发条件：

```yaml
workflow:
  - binding: authorize_rules
    unless: input.direction == egress
    when_any: [input.rule, input.protocol, input.api_param]
  - binding: authorize_rules_egress
    when: input.direction == egress
    when_any: [input.rule, input.protocol, input.api_param]
```

查询型附加 API 可以在 probe step 上使用 `merge: true`，把回读字段并入当前资源视图；`as` 可把 probe 结果保存到 `$captures.<name>.items` 供显式 output 使用。

operation input 可以覆盖这些 operation-local 属性：

- `required`
- `positional`
- `positional_many`
- `repeatable`
- `default`
- `enum`
- `description`
- `input_style`

operation input 不能覆盖 `type`、`items` 或 `fields`。

位置参数示例：

```yaml
operations:
  get:
    input:
      fields:
        - id:
            required: true
            positional: true
    workflow:
      - probe: attribute
        ids:
          - $input.id
        not_found: error
```

## 8. Filters

list operation 可以定义 filters，把通用 `--filter key=value` 映射到 operation input。

```yaml
operations:
  list:
    input:
      fields:
        - ids:
            positional_many: true
      controls:
        - filter
        - limit
        - page
    filters:
      id:
        target: ids
      name:
        target: name
      tag.:
        target: tag
        key_prefix: tag.
    workflow:
      - probe: list
        many: true
```

| 字段 | 含义 |
| --- | --- |
| `target` | 写入的 schema field 或 control 名 |
| `type` | 过滤值类型；为空时使用 target 的 schema/control 类型 |
| `key_prefix` | 动态 key 前缀，例如 `tag.` |
| `description` | 过滤字段说明 |

示例：

```bash
ecctl vpc list --filter name=prod
ecctl vpc list --filter tag.env=prod
```

## 9. Probes

`probes` 定义查询 API、请求映射和响应提取规则。probe 只负责观察资源，不直接决定命令成功或失败。

```yaml
probes:
  attribute:
    api: DescribeSecurityGroupAttribute
    request:
      RegionId: $context.region
      SecurityGroupId: $.id
    response:
      item: $
      request_id: $.RequestId
      id: $.SecurityGroupId
      state: $.Status
      fields:
        id: $.SecurityGroupId
        permissions: $.Permissions.Permission
        rules:
          from: $.Permissions.Permission
          each:
            id: $.SecurityGroupRuleId
            direction:
              lower: $.Direction
            protocol:
              lower: $.IpProtocol
            port:
              port: $.PortRange
            cidr:
              first: [$.SourceCidrIp, $.DestCidrIp]
            priority:
              int: $.Priority
```

request 表达式中，`$.name` 默认从 operation input 读取；`$context.region` 来自运行上下文。

常用响应提取：

| 规则 | 含义 |
| --- | --- |
| `items` | 多资源列表路径 |
| `item` | 单资源路径 |
| `total` | 有意义的总数路径；未声明时 list 输出省略顶层 `total`，不能用当前页数量代替。 |
| `next_token` | 下一页 token 路径；存在时 list 输出 `pagination.next_token` 且 `has_more=true`。 |
| `request_id` | request ID 路径 |
| `id` | 资源 ID 路径 |
| `state` | 状态路径 |
| `fields.<name>` | 输出字段提取规则 |
| `extra_fields.<name>` | 从响应顶层提取的附加输出字段；用于 list 这类结果主数组外还有顶层结构化数据的 API。 |
| `absent.when_empty_for_requested_ids` | 请求了 ID 但结果为空时视为 absent |

字段提取支持直接路径、`from/each`、`lower`、`first`、`int` 和 `port`。

## 10. Waiters

waiter 基于 probe 观察状态。

```yaml
waiters:
  available_after_update:
    probe: attribute
    target: Available
    interval: 2s
    timeout: 300s
    pending:
      - field: dns_hostname_status
        values: [ENABLING, DISABLING, MODIFYING]
    failure:
      states: [Failed]
```

`target: absent` 依赖 probe response 的 `absent` 规则。
`target: present` 用于等待指定 ID 出现在 probe 结果中，适合绑定关系可见性检查。

## 11. Bindings

`bindings` 表示一次写 API 调用。它负责 API 名称、请求映射、幂等 token、隐藏状态 retry、ID 提取、request ID 提取、waiter 和 hook。

```yaml
bindings:
  create_to_running:
    api: RunInstances
    hooks:
      before: [resolve_image_name]
    idempotency:
      field: ClientToken
      prefix: instance-create
    request:
      RegionId: $context.region
      ImageId: $.image
      InstanceType: $.type
      SecurityGroupId: $.sg
      VSwitchId: $.vswitch
      SystemDisk:
        from: $.system_disk
        fields:
          Category: $.category
          AutoSnapshotPolicyId: $.auto_snapshot_policy
      DataDisk:
        each: $.data_disks
        fields:
          Category: $.category
          AutoSnapshotPolicyId: $.auto_snapshot_policy
          Size: $.size
      NetworkInterface:
        each: $.network_interfaces
        fields:
          VSwitchId: $.vswitch_id
          SecurityGroupIds:
            each: $.security_group_ids
      SecurityGroupIds:
        each: $.security_group_ids
      ApiParam:
        raw: $.api_param
    id_from: $.InstanceIdSets.InstanceIdSet
    request_id_from: $.RequestId
    wait: running_after_create
```

请求映射形式：

```yaml
ApiName: $.field
```

直接映射，空值跳过。

```yaml
ApiObject:
  from: $.object_field
  fields:
    ApiChild: $.child
```

对象映射，生成 `ApiObject.ApiChild`。

```yaml
ApiArray:
  each: $.array_field
  fields:
    ApiChild: $.child
```

对象数组映射，生成 `ApiArray.1.ApiChild`、`ApiArray.2.ApiChild`。

```yaml
ApiArray:
  each: $.array_field
```

标量数组映射，生成 `ApiArray.1`、`ApiArray.2`。

```yaml
ApiParam:
  raw: $.api_param
```

兜底请求参数。解析重复的 `key=value` 输入并合并到最终请求中；默认拒绝和已映射 key 重复。

嵌套 `from` 和 `each` 会递归展开：

```yaml
NetworkInterface:
  each: $.network_interfaces
  fields:
    SecurityGroupIds:
      each: $.security_group_ids
```

生成 `NetworkInterface.1.SecurityGroupIds.1`。

## 12. Enhanced Each 与 Captures

安全组规则等输入可以先归一化再映射：

```yaml
Permissions:
  capture: rule_permissions
  each:
    normalize: security_group_rule
    sources:
      - source: $.rule
      - from_fields:
          direction: $.direction
          protocol: $.protocol
          port: $.port
          cidr: $.cidr
          policy: $.policy
          priority: $.priority
        when_any: [protocol, port, cidr]
    defaults:
      direction: ingress
      policy: accept
      priority: 1
    enum:
      direction: [ingress]
  fields:
    IpProtocol: $.protocol
    PortRange: $.port_range
    SourceCidrIp: $.cidr
    Policy: $.policy
    Priority: $.priority
```

语义：

- `sources[].source` 读取一个或多个原始输入。
- `sources[].from_fields` 从独立字段构造一个归一化对象。
- `when_any` 防止用户没传任何字段时产生全默认对象。
- `defaults` 补默认值。
- `normalize` 调用在 YAML 中显式声明的 Go hook，只用于规格无法干净表达的资源专属归一化逻辑，例如 ECS 安全组规则简写解析和端口范围派生。
- `enum` 校验归一化字段。
- `fields` 把归一化字段映射到请求参数。
- `capture` 保存归一化 items，供 output 使用。

标量 `each` 也可以 capture：

```yaml
SecurityGroupRuleId:
  each: $.rule_id
  capture:
    name: rule_ids
    fields:
      id: $.
```

## 13. Output

operation 可以使用默认输出，也可以显式声明 output。

```yaml
operations:
  authorize:
    output:
      fields:
        actions: $result.actions
        security_group:
          id: $input.id
        targets:
          value: $result.items
          when: $input.targets_for_policy
      select:
        - from: $result.item.rules
          match: $captures.rule_permissions.items
          by: [direction, protocol, port, cidr]
          fields: [id, direction, protocol, port, cidr, policy, priority]
          single_key: rule
          many_key: rules
          use_match_when_missing: true
```

支持的输出表达式根：

- `$input`
- `$context`
- `$result`
- `$captures`

`output.fields` 可以用 `value` + `when` / `unless` 包装条件字段；条件值按输出表达式是否非空判断。

## 14. Hooks

hook 是明确声明的 Go 扩展点，只用于 YAML 不应承担的跨 API 派生或错误归类。

```yaml
bindings:
  create_to_running:
    api: RunInstances
    hooks:
      before: [resolve_image_name]

  delete_to_absent:
    api: DeleteInstance
    hooks:
      after_error: [classify_delete_state_conflict]
```

普通请求映射、字段归一化、等待、输出选择都应优先写在 YAML 中。

## 15. Schema 命令输出

`ecctl schema` 支持产品、资源和命令三级自省。`ecctl schema <product>` 返回产品下的资源列表；顶层资源使用 `ecctl schema <product>.<resource>`，嵌套资源使用 `ecctl schema <product>.<parent>.<resource>`；在资源 ID 后追加 `.<action>` 返回具体命令参数。点分 ID 也可以拆成独立参数传入。

资源 surface 必须返回规范 `schema_id`，嵌套资源还应返回 `parent`。嵌套资源的规范 ID 是 `<product>.<parent>.<resource>`，命令 ID 是 `<product>.<parent>.<resource>.<action>`；不得接受省略父资源的 `<product>.<resource>[.<action>]` 形式。`cli` 字段始终给出对应的完整命令路径。这样同名子资源不会发生 schema 冲突，参考文档也可以按父资源生成唯一文件名。

`ecctl schema <command>` 从 `schema.fields`、`controls` 和 operation input 投影命令 schema。`command` 保留 schema ID；当 schema ID 不能直接当 CLI 命令执行，输出中的 `cli` 给出真实命令路径。包含位置参数的非 CRUD 动作还应输出 `schema_id`、`usage` 和 `positionals`，让 Agent 明确区分位置参数和 flags。

位置参数仍保留在 `params` 中以兼容旧消费者，但必须标记 `"positional": true` 或 `"positional_many": true`。Agent 应优先读取 `positionals` 组装 argv，不把这类字段写成 `--id` 之类的 flag。

`ecs.instance.create` 的 `data-disk` item flag 应类似：

```json
{
  "type": "object",
  "description": "Data disks to attach when creating the instance.",
  "repeatable": true,
  "input": "inline-key-value|json|@file",
  "fields": {
    "auto_snapshot_policy": {
      "type": "string",
      "description": "Automatic snapshot policy ID for the data disk."
    },
    "category": {
      "type": "string",
      "description": "Data disk category."
    }
  }
}
```

schema 输出不得泄漏底层 API 细节，也不得使用普通 `any` 类型。

brief schema 可以携带一条例子，但只用于非 CRUD 的位置参数动作，避免通用 CRUD schema 被示例文本撑大。`list` / `get` schema 可以输出紧凑 `output` 摘要：`root` 来自 `identity.output_root`，`fields` 来自所用 probe 的归一化字段名；当字段数过大时省略 `output`，保留 `--fields` 作为调用者的显式选择入口。单命令 brief schema 仍应优先控制在 2KB 内，低频字段应在 operation input 上标记 `brief: false` 并通过 `--full` 暴露。

## 16. ECS RunInstances 建模约定

ECS instance 创建应把 API 分组概念建模成嵌套资源字段：

| 资源字段 | 请求展开 |
| --- | --- |
| `arns` | `Arn.1.*` |
| `cpu_options` | `CpuOptions.*` |
| `data_disks` | `DataDisk.1.*` |
| `host_names` | `HostNames.1` |
| `ipv6_addresses` | `Ipv6Address.1` |
| `network_interfaces` | `NetworkInterface.1.*` |
| `network_interfaces.security_group_ids` | `NetworkInterface.1.SecurityGroupIds.1` |
| `security_group_ids` | `SecurityGroupIds.1` |
| `system_disk` | `SystemDisk.*` |

推荐输入：

```bash
ecctl ecs instance create \
  --system-disk category=cloud_essd,size=40 \
  --data-disk category=cloud_essd,size=100
```

不要再为同一概念新增重复的扁平字段，例如 `system_disk_category`。如果后续需要更便捷的别名，应作为 operation input alias 写入 canonical schema path，而不是复制 schema 字段。

## 17. 校验规则

loader 必须校验：

- 资源规格 `schema_version` 必须是 `2`。
- 不接受旧顶层 `params`、`actions`、`transitions`、`transforms`。
- 每个 operation input field 必须存在于 `schema.fields`。
- 每个 operation input control 必须存在于 `controls`。
- operation override 不能改变 schema type 或嵌套结构。
- workflow 中的 `binding` 必须存在于 `bindings`。
- workflow 中的 `probe` 必须存在于 `probes`。
- binding 的 `wait` 必须存在于 `waiters`。
- waiter 的 `probe` 必须存在于 `probes`。
- schema/control field 只能使用支持的类型。
- `array` 必须有 `items`。
- `object.fields` 中的子字段必须递归通过 schema 校验。

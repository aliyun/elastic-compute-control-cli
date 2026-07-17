# ack alert

资源：报警

优先级：P1

本文件只描述 `ecctl ack alert` 的 interface 级命令设计：每个操作命令对应哪些 ACK API，不展开完整 flag、参数结构和输出结构；仅在多 API 分流时标明必要的特殊开关。

设计目标：覆盖 ACK 控制面提供的报警能力，即报警规则启停与联系人、联系人组的删除/绑定更新。完整的报警规则定义、通知模板、阈值表达式由阿里云监控产品（ARMS / 云监控 CMS）维护，不在 ACK API 范围内，CLI 不强行覆盖；联系人和联系人组的创建同样由 ARMS / CMS 提供，ACK 没有 `CreateAlertContact` / `CreateAlertContactGroup`，因此 `contact` 和 `contact-group` 子命令面只保留 `delete`，`contact-group` 额外保留绑定更新动作 `update`，没有 create/list 的对称命令。需要查看或创建联系人/规则时走 ARMS / CMS。

报警开关收敛在 `update --enabled`：根据 cli-design-rules "enable/disable 用 `update --enable-*`/`--disable-*`/`--enabled`、不默认独立 action" 的规则，`StartAlert` 与 `StopAlert` 共同表达的是同一报警规则集/规则的开关状态变更，统一收敛到 `alert update --enabled=true|false`，不独立设计 `alert start` / `alert stop` 子命令。

特殊开关命名是 interface 级建议，用来说明何时触发额外 API；最终字段名和参数细节在资源 spec 中定义。

## `ecctl ack alert update`

调用 API：

- 指定 `--enabled=true` 时调用 [StartAlert](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-startalert)：开启指定集群的报警规则集或单条报警规则。
- 指定 `--enabled=false` 时调用 [StopAlert](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-stopalert)：停止指定集群的报警规则集或单条报警规则。

注意事项：报警开关表达"期望状态"，通过 `--enabled` flag 在 `StartAlert` / `StopAlert` 之间分流，不独立设计 `alert start` / `alert stop`。作用对象通过 `--rule-id` 或 `--ruleset-id` 指定规则集或单条规则，不拆 `*-rule` / `*-ruleset` 子命令。开关变更存在生效延迟时，默认等待目标状态可见并回读（如有可用的状态查询 API；当前 ACK 报警 API 不提供主动状态查询，则不做强制等待）。

## `ecctl ack alert contact delete`

调用 API：

- [DeleteAlertContact](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-deletealertcontact)：删除 ACK 报警联系人。

注意事项：子资源 `contact` 上的删除动作。ACK API 不提供 `Create` / `List`，新建和查询走 ARMS / CMS。

## `ecctl ack alert contact-group update`

调用 API：

- [UpdateContactGroupForAlert](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-updatecontactgroupforalert)：修改报警项绑定的联系人分组。

注意事项：子资源 `contact-group` 上的"绑定更新"动作，作用对象是某条报警规则或规则集与联系人组之间的关联关系，不修改联系人组本身的成员。

## `ecctl ack alert contact-group delete`

调用 API：

- [DeleteAlertContactGroup](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-deletealertcontactgroup)：删除 ACK 报警联系人分组。

注意事项：子资源 `contact-group` 上的删除动作。ACK API 不提供 `Create` / `List`，新建和查询走 ARMS / CMS。

## 暂不进入主命令面的 API

报警规则定义、联系人和联系人组的创建/查询能力由阿里云监控产品（ARMS / CMS）承载，ACK 控制面 API 没有提供对应接口，`ecctl ack alert` 不在此处兜底，需要时在对应产品的 CLI 命令面或控制台操作。

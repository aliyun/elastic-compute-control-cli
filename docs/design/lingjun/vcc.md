# lingjun vcc

资源：灵骏连接

优先级：P2

本文件只描述 `ecctl lingjun vcc` 的 interface 级命令设计：每个操作命令对应哪些 eflo API，不展开完整 flag、参数结构和输出结构；仅在多 API 分流时标明必要的特殊开关。

设计目标：覆盖灵骏连接（VCC）的全生命周期（创建/修改/删除/查询）。VCC 是灵骏网络与外部网络（如经典 VPC）之间的互联通道，支持路由管理和授权规则配置。`update` 内承载基础属性变更、路由条目和授权规则的增删操作；`get` 支持通过 `--with-routes`、`--with-grants`、`--with-flows` 追加查询关联信息。

特殊开关命名是 interface 级建议，用来说明何时触发额外 API；最终字段名和参数细节在资源 spec 中定义。

## `ecctl lingjun vcc create`

调用 API：

- [CreateVcc](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-2022-05-30-createvcc)：创建灵骏连接。
- [GetVcc](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-2022-05-30-getvcc)：轮询连接状态。

注意事项：`CreateVcc` 是异步创建接口，默认等待连接进入可用状态并回读连接视图。创建过程中如遇失败可能需要内部重试（`RetryVcc`），CLI 层自动处理。

形态：`ecctl lingjun vcc create --vpd <vpd-id> --name <name> --bandwidth <Mbps>`

## `ecctl lingjun vcc update`

调用 API：

- 默认调用 [UpdateVcc](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-2022-05-30-updatevcc)：修改连接基础属性。
- 指定 `--route +...` 时调用 [CreateVccRouteEntry](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-2022-05-30-createvccrouteentry)：为连接添加路由条目。
- 指定 `--route -<route-id>` 时调用 [DeleteVccRouteEntry](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-2022-05-30-deletevccrouteentry)：删除连接的路由条目。
- 指定 `--grant +...` 时调用 [CreateVccGrantRule](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-2022-05-30-createvccgrantrule)：为连接添加授权规则。
- 指定 `--grant -<grant-id>` 时调用 [DeleteVccGrantRule](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-2022-05-30-deletevccgrantrule)：删除连接的授权规则。
- [GetVcc](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-2022-05-30-getvcc)：回读连接变更后的资源视图。

注意事项：基础属性变更、路由条目管理和授权规则管理可在同一次 `update` 调用中组合使用。变更后默认回读连接视图，确认变更已生效。

形态：`ecctl lingjun vcc update <vcc-id> [--name ...] [--route +...] [--route -<route-id>] [--grant +...] [--grant -<grant-id>]`

## `ecctl lingjun vcc delete`

调用 API：

- [RefundVcc](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-2022-05-30-refundvcc)：退订并删除灵骏连接。
- [ListVccs](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-2022-05-30-listvccs)：确认连接删除完成。

注意事项：VCC 是预付费资源，使用 `RefundVcc` 执行退订删除。该操作是异步的，默认等待连接从列表消失或进入删除终态；高级用户可 `--no-wait` 立即返回。

形态：`ecctl lingjun vcc delete <vcc-id>`

## `ecctl lingjun vcc get`

调用 API：

- 默认调用 [GetVcc](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-2022-05-30-getvcc)：查询单个连接详情。
- 指定 `--with-routes` 时调用 [ListVccRouteEntries](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-2022-05-30-listvccrouteentries)：附带查询连接路由条目。
- 指定 `--with-grants` 时调用 [ListVccGrantRules](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-2022-05-30-listvccgrantrules)：附带查询连接授权规则。
- 指定 `--with-flows` 时调用 [ListVccFlowInfos](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-2022-05-30-listvccflowinfos)：附带查询连接流量信息。

注意事项：默认只取连接基础详情；路由条目、授权规则和流量信息通过特殊开关按需追加查询，避免 `get` 默认触发过多 API。

形态：`ecctl lingjun vcc get <vcc-id> [--with-routes] [--with-grants] [--with-flows]`

## `ecctl lingjun vcc list`

调用 API：

- [ListVccs](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-2022-05-30-listvccs)：查询灵骏连接列表。

分页约束：ListVccs 使用 PageNumber/PageSize 分页，默认 `--limit 100`，最大值以官方 API 文档为准。

形态：`ecctl lingjun vcc list [--filter ...] [--limit 100]`

## 暂不进入主命令面的 API

以下 API 已调研，但不作为 `ecctl lingjun vcc` 的 80% 主命令能力；需要时可先通过原始 OpenAPI 兜底。

- [InitializeVcc](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-2022-05-30-initializevcc)：VCC 初始化为一次性操作，不需要日常 CLI 入口。
- [DescribeSlr](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-2022-05-30-describeslr)：服务关联角色查询为内部依赖，不独立暴露。
- [RetryVcc](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-2022-05-30-retryvcc)：VCC 重试为内部容错机制，由 CLI 自动处理，不独立暴露。
- [GetVccRouteEntry](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-2022-05-30-getvccrouteentry)：单条路由详情已通过 `get --with-routes` 列表覆盖；需要时走原始 OpenAPI。
- [GetVccGrantRule](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-2022-05-30-getvccgrantrule)：单条授权规则详情已通过 `get --with-grants` 列表覆盖；需要时走原始 OpenAPI。

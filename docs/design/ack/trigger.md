# ack trigger

资源：应用触发器

优先级：P2

别名：无

本文件只描述 `ecctl ack trigger` 的 interface 级命令设计：每个操作命令对应哪些 ACK API，不展开完整 flag、参数结构和输出结构；仅在多 API 分流时标明必要的特殊开关。

设计目标：覆盖 ACK 应用触发器的常用治理场景。触发器是 Webhook 风格的入口，常见用法是被外部 CI/CD 系统调用以拉起集群中应用的 redeploy。ACK 只暴露 `create` / `delete` 与单一查询 API（`DescribeTrigger`，既支持单查也支持列表），没有 `update` —— 修改触发器需要重建（先 `delete` 再 `create`，调用方需要同步更新新返回的 token URL）。CLI 仍同时保留 `get` 与 `list` 两个标准动作，`get` 按触发器 ID 收敛到单条详情，`list` 按集群 / 应用过滤；两者共用 `DescribeTrigger`，由 CLI 侧按是否传入 ID 区分。

特殊开关命名是 interface 级建议，用来说明何时触发额外 API；最终字段名和参数细节在资源 spec 中定义。

## `ecctl ack trigger create`

调用 API：

- [CreateTrigger](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-createtrigger)：为指定应用创建触发器，返回 token URL。

注意事项：`create` 使用 `CreateTrigger`，输出包含可被外部 CI/CD 直接调用的 token URL。同一应用可以创建多个触发器以服务不同调用方。创建是同步操作，不需要等待。

## `ecctl ack trigger delete`

调用 API：

- [DeleteTrigger](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-deletetrigger)：删除触发器。

注意事项：触发器没有 `update` 语义；CLI 不封装 `delete` + `create` 的"重建"组合，避免破坏幂等。调整触发器配置时由调用方显式按 `delete` → `create` 顺序操作，重建会得到新的 token URL，外部 CI/CD 需要同步更新。删除是同步操作，不需要等待。

## `ecctl ack trigger get`

调用 API：

- [DescribeTrigger](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-describetrigger)：按触发器 ID 查询单条记录。

注意事项：ACK 没有独立的单查接口，`DescribeTrigger` 同时承担列表和单查；`get` 复用该 API，通过触发器 ID 收敛到单条详情，与 `list` 共用底层调用，但语义按主详情区分，与 ecs `command get` / `node get` 风格一致。

## `ecctl ack trigger list`

调用 API：

- [DescribeTrigger](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-describetrigger)：列出符合条件的触发器（按集群 / 应用过滤）。

注意事项：`list` 默认按集群 / 应用维度过滤，返回多条触发器记录；单条详情走 `get <id>`。

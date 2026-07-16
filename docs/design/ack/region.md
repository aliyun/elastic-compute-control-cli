# ack region

资源：地域

优先级：P2

别名：无

本文件只描述 `ecctl ack region` 的 interface 级命令设计：每个操作命令对应哪些 ACK API，不展开 flag、参数结构和输出结构。

## `ecctl ack region list`

调用 API：

- [DescribeRegions](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-describeregions)：列出 ACK 支持的地域。

注意事项：元数据查询，无异步任务模型。返回作为 `cluster create/upgrade` 等命令的前置校验数据源。ACK 单独维护地域元数据、不复用 `ecs region` 的跨产品约定见 [README.md](README.md)。

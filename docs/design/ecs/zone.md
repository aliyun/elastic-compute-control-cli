# ecs zone

资源：可用区

优先级：P0

本文件只描述 `ecctl ecs zone` 的 interface 级命令设计：每个操作命令对应哪些 ECS API，不展开 flag、参数结构和输出结构。

## `ecctl ecs zone list`

调用 API：

- [DescribeZones](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describezones)：查询可用区列表。

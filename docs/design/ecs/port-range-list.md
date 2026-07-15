# ecs port-range-list

资源：端口列表

优先级：P2

别名：`prl`

本文件只描述 `ecctl ecs port-range-list` 的 interface 级命令设计：每个操作命令对应哪些 ECS API，不展开 flag、参数结构和输出结构。

## `ecctl ecs port-range-list create`

调用 API：

- [CreatePortRangeList](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-createportrangelist)：创建端口列表。

## `ecctl ecs port-range-list update`

调用 API：

- [ModifyPortRangeList](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-modifyportrangelist)：修改端口列表的属性。
- [DescribePortRangeLists](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describeportrangelists)：回读端口列表。
- [DescribePortRangeListEntries](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describeportrangelistentries)：回读端口列表条目。

注意事项：端口列表属性或条目修改后默认回读资源视图；存在关联资源传播延迟时，默认等待目标条目可见。

## `ecctl ecs port-range-list delete`

调用 API：

- [DeletePortRangeList](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-deleteportrangelist)：删除端口列表。

## `ecctl ecs port-range-list get`

调用 API：

- [DescribePortRangeLists](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describeportrangelists)：查询端口列表。
- [DescribePortRangeListAssociations](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describeportrangelistassociations)：查询端口列表已关联的资源信息。
- [DescribePortRangeListEntries](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describeportrangelistentries)：查询端口列表的条目。

注意事项：该查询复用列表 API，通过资源标识或过滤条件收敛到单个资源。 指定 `--with-associations` 时附带查询关联资源。 指定 `--with-entries` 时附带查询端口条目。

## `ecctl ecs port-range-list list`

调用 API：

- [DescribePortRangeLists](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describeportrangelists)：查询端口列表。

# ecs prefix-list

资源：前缀列表

优先级：P2

别名：`pl`

本文件只描述 `ecctl ecs prefix-list` 的 interface 级命令设计：每个操作命令对应哪些 ECS API，不展开 flag、参数结构和输出结构。

## `ecctl ecs prefix-list create`

调用 API：

- [CreatePrefixList](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-createprefixlist)：创建前缀列表。

## `ecctl ecs prefix-list update`

调用 API：

- [ModifyPrefixList](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-modifyprefixlist)：修改前缀列表的属性。
- [DescribePrefixListAttributes](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describeprefixlistattributes)：回读前缀列表详细信息。

注意事项：前缀列表属性或条目修改后默认回读资源视图；存在关联资源传播延迟时，默认等待目标条目可见。

## `ecctl ecs prefix-list delete`

调用 API：

- [DeletePrefixList](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-deleteprefixlist)：删除前缀列表。

## `ecctl ecs prefix-list get`

调用 API：

- [DescribePrefixListAttributes](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describeprefixlistattributes)：查询前缀列表的详细信息。
- [DescribePrefixListAssociations](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describeprefixlistassociations)：查询前缀列表已关联的资源信息。

注意事项：默认查询前缀列表详情；指定 `--with-associations` 时附带查询关联资源。

## `ecctl ecs prefix-list list`

调用 API：

- [DescribePrefixLists](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describeprefixlists)：查询前缀列表。

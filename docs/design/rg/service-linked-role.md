# rg service-linked-role

资源：服务关联角色

优先级：P3

别名：`slr`

本文件描述 `ecctl rg service-linked-role` 的 interface 级命令设计：每个操作命令对应哪些资源管理 API，不展开完整参数结构和输出结构；多 API 命令需写明触发 flag。

## `ecctl rg service-linked-role create`

调用 API：

- [CreateServiceLinkedRole](https://help.aliyun.com/zh/resource-management/resource-group/developer-reference/api-resourcemanager-2020-03-31-createservicelinkedrole-rg)：创建服务关联角色。

## `ecctl rg service-linked-role delete`

调用 API：

- [DeleteServiceLinkedRole](https://help.aliyun.com/zh/resource-management/resource-group/developer-reference/api-resourcemanager-2020-03-31-deleteservicelinkedrole-rg)：删除服务关联角色。
- [GetServiceLinkedRoleDeletionStatus](https://help.aliyun.com/zh/resource-management/resource-group/developer-reference/api-resourcemanager-2020-03-31-getservicelinkedroledeletionstatus-rg)：获取删除任务状态。

注意事项：删除服务关联角色是异步任务时，默认等待删除完成并回读任务状态。

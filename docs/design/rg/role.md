# rg role

资源：角色

优先级：P3

本文件描述 `ecctl rg role` 的 interface 级命令设计：每个操作命令对应哪些资源管理 API，不展开完整参数结构和输出结构；多 API 命令需写明触发 flag。

## `ecctl rg role create`

调用 API：

- [CreateRole](https://help.aliyun.com/zh/resource-management/resource-group/developer-reference/api-resourcemanager-2020-03-31-createrole-rg)：创建角色。

## `ecctl rg role update`

调用 API：

- [UpdateRole](https://help.aliyun.com/zh/resource-management/resource-group/developer-reference/api-resourcemanager-2020-03-31-updaterole-rg)：更新角色信息。
- [GetRole](https://help.aliyun.com/zh/resource-management/resource-group/developer-reference/api-resourcemanager-2020-03-31-getrole-rg)：回读角色信息。

注意事项：角色更新后默认回读角色信息。

## `ecctl rg role delete`

调用 API：

- [DeleteRole](https://help.aliyun.com/zh/resource-management/resource-group/developer-reference/api-resourcemanager-2020-03-31-deleterole-rg)：删除角色。

## `ecctl rg role get`

调用 API：

- [GetRole](https://help.aliyun.com/zh/resource-management/resource-group/developer-reference/api-resourcemanager-2020-03-31-getrole-rg)：获取角色信息。

## `ecctl rg role list`

调用 API：

- [ListRoles](https://help.aliyun.com/zh/resource-management/resource-group/developer-reference/api-resourcemanager-2020-03-31-listroles-rg)：查看角色列表。

# rg admin-setting

资源：资源组管理员配置

优先级：P3

本文件描述 `ecctl rg admin-setting` 的 interface 级命令设计：每个操作命令对应哪些资源管理 API，不展开完整参数结构和输出结构；多 API 命令需写明触发 flag。

## `ecctl rg admin-setting update`

调用 API：

- [UpdateResourceGroupAdminSetting](https://help.aliyun.com/zh/resource-management/resource-group/developer-reference/api-resourcemanager-2020-03-31-updateresourcegroupadminsetting-rg)：更新资源组管理员配置。
- [GetResourceGroupAdminSetting](https://help.aliyun.com/zh/resource-management/resource-group/developer-reference/api-resourcemanager-2020-03-31-getresourcegroupadminsetting-rg)：回读资源组管理员配置。

注意事项：管理员配置更新后默认回读配置。

## `ecctl rg admin-setting get`

调用 API：

- [GetResourceGroupAdminSetting](https://help.aliyun.com/zh/resource-management/resource-group/developer-reference/api-resourcemanager-2020-03-31-getresourcegroupadminsetting-rg)：查询资源组管理员配置。

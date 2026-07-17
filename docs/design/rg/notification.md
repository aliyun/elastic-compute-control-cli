# rg notification

资源：资源组通知

优先级：P3

别名：`notify`

本文件描述 `ecctl rg notification` 的 interface 级命令设计：每个操作命令对应哪些资源管理 API，不展开完整参数结构和输出结构；多 API 命令需写明触发 flag。

## `ecctl rg notification get`

调用 API：

- [GetResourceGroupNotificationSetting](https://help.aliyun.com/zh/resource-management/resource-group/developer-reference/api-resourcemanager-2020-03-31-getresourcegroupnotificationsetting-rg)：查询资源组通知设置。

## `ecctl rg notification enable`

调用 API：

- [EnableResourceGroupNotification](https://help.aliyun.com/zh/resource-management/resource-group/developer-reference/api-resourcemanager-2020-03-31-enableresourcegroupnotification-rg)：开通资源组事件通知。
- [GetResourceGroupNotificationSetting](https://help.aliyun.com/zh/resource-management/resource-group/developer-reference/api-resourcemanager-2020-03-31-getresourcegroupnotificationsetting-rg)：回读资源组通知设置。

注意事项：开通后默认回读通知设置。

## `ecctl rg notification disable`

调用 API：

- [DisableResourceGroupNotification](https://help.aliyun.com/zh/resource-management/resource-group/developer-reference/api-resourcemanager-2020-03-31-disableresourcegroupnotification-rg)：关闭资源组通知。
- [GetResourceGroupNotificationSetting](https://help.aliyun.com/zh/resource-management/resource-group/developer-reference/api-resourcemanager-2020-03-31-getresourcegroupnotificationsetting-rg)：回读资源组通知设置。

注意事项：关闭后默认回读通知设置。

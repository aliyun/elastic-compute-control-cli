# rg associated-transfer

资源：关联资源跟随转组

优先级：P3

别名：`at`

本文件描述 `ecctl rg associated-transfer` 的 interface 级命令设计：每个操作命令对应哪些资源管理 API，不展开完整参数结构和输出结构；多 API 命令需写明触发 flag。

## `ecctl rg associated-transfer update`

调用 API：

- [UpdateAssociatedTransferSetting](https://help.aliyun.com/zh/resource-management/resource-group/developer-reference/api-resourcemanager-2020-03-31-updateassociatedtransfersetting-rg)：更新关联转组功能设置。
- [ListAssociatedTransferSetting](https://help.aliyun.com/zh/resource-management/resource-group/developer-reference/api-resourcemanager-2020-03-31-listassociatedtransfersetting-rg)：回读关联转组功能设置。

注意事项：更新后默认回读关联转组设置。

## `ecctl rg associated-transfer list`

调用 API：

- [ListAssociatedTransferSetting](https://help.aliyun.com/zh/resource-management/resource-group/developer-reference/api-resourcemanager-2020-03-31-listassociatedtransfersetting-rg)：获取关联转组功能设置。

## `ecctl rg associated-transfer enable`

调用 API：

- [EnableAssociatedTransfer](https://help.aliyun.com/zh/resource-management/resource-group/developer-reference/api-resourcemanager-2020-03-31-enableassociatedtransfer-rg)：开通关联资源跟随转组功能。
- [ListAssociatedTransferSetting](https://help.aliyun.com/zh/resource-management/resource-group/developer-reference/api-resourcemanager-2020-03-31-listassociatedtransfersetting-rg)：回读关联转组功能设置。

注意事项：开通后默认回读关联转组设置。

## `ecctl rg associated-transfer disable`

调用 API：

- [DisableAssociatedTransfer](https://help.aliyun.com/zh/resource-management/resource-group/developer-reference/api-resourcemanager-2020-03-31-disableassociatedtransfer-rg)：关闭关联资源跟随转组功能。
- [ListAssociatedTransferSetting](https://help.aliyun.com/zh/resource-management/resource-group/developer-reference/api-resourcemanager-2020-03-31-listassociatedtransfersetting-rg)：回读关联转组功能设置。

注意事项：关闭后默认回读关联转组设置。

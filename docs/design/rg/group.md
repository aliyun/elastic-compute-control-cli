# rg group

资源：资源组

优先级：P1

本文件描述 `ecctl rg group` 的 interface 级命令设计：每个操作命令对应哪些资源管理 API，不展开完整参数结构和输出结构；多 API 命令需写明触发 flag。

`rg` 是和 `ecs` 平级的资源组治理产品。`group` 只承载资源组自身管理；资源列表、转组、事件、能力项和管理员配置分别放在对应资源文件中，不在本资源下声明额外关系命令。

## `ecctl rg group create`

调用 API：

- [CreateResourceGroup](https://help.aliyun.com/zh/resource-management/resource-group/developer-reference/api-resourcemanager-2020-03-31-createresourcegroup-rg)：创建资源组。
- [GetResourceGroup](https://help.aliyun.com/zh/resource-management/resource-group/developer-reference/api-resourcemanager-2020-03-31-getresourcegroup-rg)：回读资源组信息。

注意事项：创建后默认回读资源组信息，确认资源组可见。

## `ecctl rg group update`

调用 API：

- [UpdateResourceGroup](https://help.aliyun.com/zh/resource-management/resource-group/developer-reference/api-resourcemanager-2020-03-31-updateresourcegroup-rg)：修改资源组显示名称。
- [GetResourceGroup](https://help.aliyun.com/zh/resource-management/resource-group/developer-reference/api-resourcemanager-2020-03-31-getresourcegroup-rg)：回读资源组信息。

注意事项：修改资源组自身属性并回读资源组信息。跨产品批量转组使用 `ecctl rg resource update`。

## `ecctl rg group delete`

调用 API：

- [DeleteResourceGroup](https://help.aliyun.com/zh/resource-management/resource-group/developer-reference/api-resourcemanager-2020-03-31-deleteresourcegroup-rg)：删除资源组。

## `ecctl rg group get`

调用 API：

- [GetResourceGroup](https://help.aliyun.com/zh/resource-management/resource-group/developer-reference/api-resourcemanager-2020-03-31-getresourcegroup-rg)：查询资源组信息。
- [GetResourceGroupResourceCounts](https://help.aliyun.com/zh/resource-management/resource-group/developer-reference/api-resourcemanager-2020-03-31-getresourcegroupresourcecounts-rg)：查询可见资源组下的资源数量。

注意事项：默认查询资源组信息；指定 `--with-counts` 时附带查询资源数量。资源列表使用 `ecctl rg resource list`。

## `ecctl rg group list`

调用 API：

- [ListResourceGroups](https://help.aliyun.com/zh/resource-management/resource-group/developer-reference/api-resourcemanager-2020-03-31-listresourcegroups-rg)：查询资源组列表。
- [ListResourceGroupsWithAuthDetails](https://help.aliyun.com/zh/resource-management/resource-group/developer-reference/api-resourcemanager-2020-03-31-listresourcegroupswithauthdetails-rg)：列出资源组与用户授权信息。

注意事项：默认查询资源组列表；指定 `--with-auth-details` 时附带授权信息。

分页约束：`ListResourceGroups` 的 `PageSize` 取值范围为 1~100；`ListResourceGroupsWithAuthDetails` 文档未显式给出上限，按同资源列表上限 100 对齐，`list` 默认 `--limit 100`。

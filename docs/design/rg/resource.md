# rg resource

资源：资源组内资源

优先级：P1

本文件描述 `ecctl rg resource` 的 interface 级命令设计：每个操作命令对应哪些资源管理 API；多 API 命令写明触发 flag，不展开完整参数结构和输出结构。

## `ecctl rg resource update`

调用 API：

- [MoveResources](https://help.aliyun.com/zh/resource-management/resource-group/developer-reference/api-resourcemanager-2020-03-31-moveresources-rg)：将资源转移到目标资源组。
- [ListResources](https://help.aliyun.com/zh/resource-management/resource-group/developer-reference/api-resourcemanager-2020-03-31-listresources-rg)：回读资源组内资源列表。

触发规则：`update <target-group-id>` 调用 `MoveResources`；默认随后用 `ListResources` 等待 `--resource` 指定的资源出现在目标资源组。`--no-wait` 跳过等待和回读，`--timeout` 控制等待超时。

注意事项：这是跨产品批量转组入口，不替代具体业务资源的 `update --resource-group` 主路径。

## `ecctl rg resource list`

调用 API：

- [ListResources](https://help.aliyun.com/zh/resource-management/resource-group/developer-reference/api-resourcemanager-2020-03-31-listresources-rg)：查看当前账号可以访问的资源列表。

触发规则：筛选条件统一通过 `--filter` 传入，例如 `--filter service=ecs`、`--filter resource-type=instance`、`--filter resource-id=i-xxx`。

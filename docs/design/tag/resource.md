# tag resource

资源：跨产品资源标签

优先级：P1

本文件描述 `ecctl tag resource` 的 interface 级命令设计：每个操作命令对应哪些标签服务 API；多 API 命令写明触发 flag，不展开完整参数结构和输出结构。

`tag` 是和 `ecs` 平级的全局治理产品，用于跨产品、批量或标签目录视角的标签管理；单个业务资源的标签关系仍优先放在对应资源的 `update` 中，例如 `ecs instance update --tag k=v`。

## `ecctl tag resource list`

调用 API：

- [ListTagResources](https://help.aliyun.com/zh/resource-management/tag/developer-reference/api-tag-2018-08-28-listtagresources)：查询多个云产品的多个云资源绑定的标签列表。
- [ListResourcesByTag](https://help.aliyun.com/zh/resource-management/tag/developer-reference/api-tag-2018-08-28-listresourcesbytag)：基于标签查询资源。

触发规则：默认调用 `ListTagResources` 查询指定资源绑定的标签，资源 ARN 通过 `--filter arn=...` 传入；指定 `--resource-type ...`、`--fuzzy-type ...` 或 `--include-all-tags` 时调用 `ListResourcesByTag`，标签反查条件通过 `--filter tag.<key>=<value>` 传入。

注意事项：按标签反查资源不单独设计 `search` 命令。

分页约束：`ListTagResources` 的 `PageSize` 和 `ListResourcesByTag` 的 `MaxResult` 最大值均为 1000；按通用设计默认不提升到 API 上限，`list` 默认 `--limit 100`。

## `ecctl tag resource apply`

调用 API：

- [TagResources](https://help.aliyun.com/zh/resource-management/tag/developer-reference/api-tag-2018-08-28-tagresources)：为多个云产品的多个云资源统一创建并绑定标签。

注意事项：这是跨产品批量标签操作，不替代具体资源的 `update --tag` 主路径。`TagResources` 返回后不额外调用 list 回读。

## `ecctl tag resource remove`

调用 API：

- [UntagResources](https://help.aliyun.com/zh/resource-management/tag/developer-reference/api-tag-2018-08-28-untagresources)：为多个云产品的多个云资源统一解绑标签。

注意事项：`UntagResources` 返回后不额外调用 list 回读。

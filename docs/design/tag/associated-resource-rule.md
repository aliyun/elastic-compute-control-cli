# tag associated-resource-rule

资源：关联资源标签规则

优先级：P3

别名：`arr`

本文件描述 `ecctl tag associated-resource-rule` 的 interface 级命令设计：每个操作命令对应哪些标签服务 API，不展开完整参数结构和输出结构；多 API 命令需写明触发 flag。

## `ecctl tag associated-resource-rule create`

调用 API：

- [CreateAssociatedResourceRules](https://help.aliyun.com/zh/resource-management/tag/developer-reference/api-tag-2018-08-28-createassociatedresourcerules)：创建关联资源标签规则。

## `ecctl tag associated-resource-rule update`

调用 API：

- [UpdateAssociatedResourceRule](https://help.aliyun.com/zh/resource-management/tag/developer-reference/api-tag-2018-08-28-updateassociatedresourcerule)：更新关联资源标签规则。
- [ListAssociatedResourceRules](https://help.aliyun.com/zh/resource-management/tag/developer-reference/api-tag-2018-08-28-listassociatedresourcerules)：回读关联资源标签规则列表。

注意事项：规则更新后默认回读规则列表，确认变更结果可见。

## `ecctl tag associated-resource-rule delete`

调用 API：

- [DeleteAssociatedResourceRule](https://help.aliyun.com/zh/resource-management/tag/developer-reference/api-tag-2018-08-28-deleteassociatedresourcerule)：删除关联资源标签规则。

## `ecctl tag associated-resource-rule list`

调用 API：

- [ListAssociatedResourceRules](https://help.aliyun.com/zh/resource-management/tag/developer-reference/api-tag-2018-08-28-listassociatedresourcerules)：查询关联资源标签规则列表。

分页约束：`ListAssociatedResourceRules` 的 `MaxResult` 最大值为 100，`list` 默认 `--limit 100`。

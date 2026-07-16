# tag policy

资源：标签策略

优先级：P3

本文件描述 `ecctl tag policy` 的 interface 级命令设计：每个操作命令对应哪些标签服务 API；多 API 命令写明触发 flag，不展开完整参数结构和输出结构。

## `ecctl tag policy create`

调用 API：

- [CreatePolicy](https://help.aliyun.com/zh/resource-management/tag/developer-reference/api-tag-2018-08-28-createpolicy)：创建标签策略。

## `ecctl tag policy update`

调用 API：

- [ModifyPolicy](https://help.aliyun.com/zh/resource-management/tag/developer-reference/api-tag-2018-08-28-modifypolicy)：修改标签策略。
- [GetPolicy](https://help.aliyun.com/zh/resource-management/tag/developer-reference/api-tag-2018-08-28-getpolicy)：回读标签策略详情。

注意事项：策略修改后默认回读策略详情，确认变更结果可见。

## `ecctl tag policy delete`

调用 API：

- [DeletePolicy](https://help.aliyun.com/zh/resource-management/tag/developer-reference/api-tag-2018-08-28-deletepolicy)：删除标签策略。

## `ecctl tag policy get`

调用 API：

- [GetPolicy](https://help.aliyun.com/zh/resource-management/tag/developer-reference/api-tag-2018-08-28-getpolicy)：查询标签策略详情。
- [GetPolicyEnableStatus](https://help.aliyun.com/zh/resource-management/tag/developer-reference/api-tag-2018-08-28-getpolicyenablestatus)：查询标签策略状态。
- [GetEffectivePolicy](https://help.aliyun.com/zh/resource-management/tag/developer-reference/api-tag-2018-08-28-geteffectivepolicy)：查询有效策略。

触发规则：默认调用 `GetPolicy` 查询策略详情；指定 `--with-status` 时附带调用 `GetPolicyEnableStatus`；指定 `--with-effective` 时附带调用 `GetEffectivePolicy`，并通过 `--target`、`--target-type` 和 `--tag-keys` 传入有效策略查询上下文。

## `ecctl tag policy list`

调用 API：

- [ListPolicies](https://help.aliyun.com/zh/resource-management/tag/developer-reference/api-tag-2018-08-28-listpolicies)：查询标签策略列表。
- [ListPoliciesForTarget](https://help.aliyun.com/zh/resource-management/tag/developer-reference/api-tag-2018-08-28-listpoliciesfortarget)：查询目标节点绑定的标签策略列表。
- [ListTargetsForPolicy](https://help.aliyun.com/zh/resource-management/tag/developer-reference/api-tag-2018-08-28-listtargetsforpolicy)：查询标签策略绑定的目标节点。

触发规则：默认调用 `ListPolicies`；指定 `--target` 与 `--target-type` 时调用 `ListPoliciesForTarget`；指定 `--targets-for-policy <policy-id>` 时调用 `ListTargetsForPolicy`。

分页约束：这三个 API 的 `MaxResult` 最大值均为 1000；按通用设计默认不提升到 API 上限，`list` 默认 `--limit 100`。

## `ecctl tag policy attach`

调用 API：

- [AttachPolicy](https://help.aliyun.com/zh/resource-management/tag/developer-reference/api-tag-2018-08-28-attachpolicy)：将标签策略绑定到目标节点。
- [ListPoliciesForTarget](https://help.aliyun.com/zh/resource-management/tag/developer-reference/api-tag-2018-08-28-listpoliciesfortarget)：回读目标节点绑定的标签策略列表。

注意事项：绑定后默认等待关系可见并回读目标节点策略列表。

## `ecctl tag policy detach`

调用 API：

- [DetachPolicy](https://help.aliyun.com/zh/resource-management/tag/developer-reference/api-tag-2018-08-28-detachpolicy)：解绑指定的标签策略。
- [ListPoliciesForTarget](https://help.aliyun.com/zh/resource-management/tag/developer-reference/api-tag-2018-08-28-listpoliciesfortarget)：回读目标节点绑定的标签策略列表。

注意事项：解绑后默认等待关系更新可见并回读目标节点策略列表。

## 暂不支持的 API

以下标签策略检测和报告能力先不进入 `ecctl tag policy` 命令面，后续如有明确使用场景再设计独立命令或并入现有查询：

- [ListConfigRulesForTarget](https://help.aliyun.com/zh/resource-management/tag/developer-reference/api-tag-2018-08-28-listconfigrulesfortarget)：查询目标节点的标签检测任务列表。
- [GenerateConfigRuleReport](https://help.aliyun.com/zh/resource-management/tag/developer-reference/api-tag-2018-08-28-generateconfigrulereport)：生成不合规资源检测报告。
- [GetConfigRuleReport](https://help.aliyun.com/zh/resource-management/tag/developer-reference/api-tag-2018-08-28-getconfigrulereport)：查询不合规资源检测报告的基本信息。

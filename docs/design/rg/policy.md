# rg policy

资源：权限策略

优先级：P3

本文件描述 `ecctl rg policy` 的 interface 级命令设计：每个操作命令对应哪些资源管理 API；多 API 命令写明触发 flag，不展开完整参数结构和输出结构。

## `ecctl rg policy create`

调用 API：

- [CreatePolicy](https://help.aliyun.com/zh/resource-management/resource-group/developer-reference/api-resourcemanager-2020-03-31-createpolicy-rg)：创建权限策略。

## `ecctl rg policy delete`

调用 API：

- [DeletePolicy](https://help.aliyun.com/zh/resource-management/resource-group/developer-reference/api-resourcemanager-2020-03-31-deletepolicy-rg)：删除权限策略。

## `ecctl rg policy get`

调用 API：

- [GetPolicy](https://help.aliyun.com/zh/resource-management/resource-group/developer-reference/api-resourcemanager-2020-03-31-getpolicy-rg)：获取指定的权限策略信息。

## `ecctl rg policy list`

调用 API：

- [ListPolicies](https://help.aliyun.com/zh/resource-management/resource-group/developer-reference/api-resourcemanager-2020-03-31-listpolicies-rg)：查看权限策略列表。
- [ListPolicyAttachments](https://help.aliyun.com/zh/resource-management/resource-group/developer-reference/api-resourcemanager-2020-03-31-listpolicyattachments-rg)：查看授权列表。

触发规则：默认调用 `ListPolicies`；指定 `--resource-group`、`--principal-type` 或 `--principal-name` 时调用 `ListPolicyAttachments`。

## `ecctl rg policy attach`

调用 API：

- [AttachPolicy](https://help.aliyun.com/zh/resource-management/resource-group/developer-reference/api-resourcemanager-2020-03-31-attachpolicy-rg)：为 RAM 身份授权。
- [ListPolicyAttachments](https://help.aliyun.com/zh/resource-management/resource-group/developer-reference/api-resourcemanager-2020-03-31-listpolicyattachments-rg)：回读授权列表。

注意事项：授权后默认等待关系可见并回读授权列表；`--no-wait` 跳过等待和回读，`--timeout` 控制等待超时。

## `ecctl rg policy detach`

调用 API：

- [DetachPolicy](https://help.aliyun.com/zh/resource-management/resource-group/developer-reference/api-resourcemanager-2020-03-31-detachpolicy-rg)：为 RAM 身份移除权限。
- [ListPolicyAttachments](https://help.aliyun.com/zh/resource-management/resource-group/developer-reference/api-resourcemanager-2020-03-31-listpolicyattachments-rg)：回读授权列表。

注意事项：移除授权后默认等待关系更新可见并回读授权列表；`--no-wait` 跳过等待和回读，`--timeout` 控制等待超时。

## `ecctl rg policy version create`

调用 API：

- [CreatePolicyVersion](https://help.aliyun.com/zh/resource-management/resource-group/developer-reference/api-resourcemanager-2020-03-31-createpolicyversion-rg)：创建权限策略版本。

## `ecctl rg policy version update`

调用 API：

- [SetDefaultPolicyVersion](https://help.aliyun.com/zh/resource-management/resource-group/developer-reference/api-resourcemanager-2020-03-31-setdefaultpolicyversion-rg)：设置权限策略默认版本。
- [GetPolicy](https://help.aliyun.com/zh/resource-management/resource-group/developer-reference/api-resourcemanager-2020-03-31-getpolicy-rg)：回读权限策略信息。

触发规则：指定 `--set-as-default` 时调用 `SetDefaultPolicyVersion` 切换默认版本，并调用 `GetPolicy` 回读策略信息。

## `ecctl rg policy version delete`

调用 API：

- [DeletePolicyVersion](https://help.aliyun.com/zh/resource-management/resource-group/developer-reference/api-resourcemanager-2020-03-31-deletepolicyversion-rg)：删除权限策略版本。

## `ecctl rg policy version get`

调用 API：

- [GetPolicyVersion](https://help.aliyun.com/zh/resource-management/resource-group/developer-reference/api-resourcemanager-2020-03-31-getpolicyversion-rg)：获取权限策略版本。

## `ecctl rg policy version list`

调用 API：

- [ListPolicyVersions](https://help.aliyun.com/zh/resource-management/resource-group/developer-reference/api-resourcemanager-2020-03-31-listpolicyversions-rg)：查看权限策略版本列表。

注意事项：权限策略版本是权限策略的附属对象，作为 `policy` 的 `version` 子命令承载。

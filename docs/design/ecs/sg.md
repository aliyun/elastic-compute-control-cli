# ecs sg

资源：安全组

优先级：P0

本文件只描述 `ecctl ecs sg` 的 interface 级命令设计：每个操作命令对应哪些 ECS API，不展开 flag、参数结构和输出结构。

安全组资源承载安全组自身、入方向/出方向规则和规则引用查询。实例加入/离开安全组属于实例侧关系操作，放在 instance 资源文档。

## `ecctl ecs sg create`

调用 API：

- [CreateSecurityGroup](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-createsecuritygroup)：创建安全组。

## `ecctl ecs sg update`

调用 API：

- [ModifySecurityGroupAttribute](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-modifysecuritygroupattribute)：修改安全组的名称或者描述。
- [ModifySecurityGroupPolicy](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-modifysecuritygrouppolicy)：修改普通安全组的组内连通策略。
- [ModifySecurityGroupRule](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-modifysecuritygrouprule)：修改安全组入方向规则。
- [ModifySecurityGroupEgressRule](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-modifysecuritygroupegressrule)：修改安全组出方向规则。
- [DescribeSecurityGroupAttribute](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describesecuritygroupattribute)：回读安全组和组内规则信息。

注意事项：安全组属性、组内连通策略、入方向规则和出方向规则修改都归入 `update`，不再独立设计 `rule update`、`egress-rule update`、`policy update` 子命令。规则和策略变更后默认单次回读安全组属性，不额外等待规则传播完成。

## `ecctl ecs sg delete`

调用 API：

- [DeleteSecurityGroup](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-deletesecuritygroup)：删除安全组。

## `ecctl ecs sg get`

调用 API：

- [DescribeSecurityGroupAttribute](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describesecuritygroupattribute)：查询安全组和组内规则信息。
- [DescribeSecurityGroupReferences](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describesecuritygroupreferences)：查询被授权的安全组列表。

注意事项：默认查询安全组属性和规则；指定 `--with-references` 时附带查询安全组引用关系。

## `ecctl ecs sg list`

调用 API：

- [DescribeSecurityGroups](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describesecuritygroups)：查询安全组基本信息列表。

分页约束：`DescribeSecurityGroups` 使用 `MaxResults/NextToken`，`list` 暴露 `--next-token/--limit`。`MaxResults` 最大值为 100，默认 `--limit 100`。

## `ecctl ecs sg authorize`

调用 API：

- [AuthorizeSecurityGroup](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-authorizesecuritygroup)：增加安全组入方向规则。
- [AuthorizeSecurityGroupEgress](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-authorizesecuritygroupegress)：增加安全组出方向规则。

注意事项：默认 `--direction ingress`，调用 `AuthorizeSecurityGroup` 增加入方向规则；指定 `--direction egress` 时调用 `AuthorizeSecurityGroupEgress` 增加出方向规则。

## `ecctl ecs sg revoke`

调用 API：

- [RevokeSecurityGroup](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-revokesecuritygroup)：删除安全组入方向规则。
- [RevokeSecurityGroupEgress](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-revokesecuritygroupegress)：删除出方向安全组规则。

注意事项：默认 `--direction ingress`，调用 `RevokeSecurityGroup` 删除入方向规则；指定 `--direction egress` 时调用 `RevokeSecurityGroupEgress` 删除出方向规则。

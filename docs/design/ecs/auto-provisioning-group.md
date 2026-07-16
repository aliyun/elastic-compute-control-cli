# ecs auto-provisioning-group

资源：弹性供应组

优先级：P3

别名：`apg`

本文件只描述 `ecctl ecs auto-provisioning-group` 的 interface 级命令设计：每个操作命令对应哪些 ECS API，不展开 flag、参数结构和输出结构。

## `ecctl ecs auto-provisioning-group create`

调用 API：

- [CreateAutoProvisioningGroup](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-createautoprovisioninggroup)：创建弹性供应组。

## `ecctl ecs auto-provisioning-group update`

调用 API：

- [ModifyAutoProvisioningGroup](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-modifyautoprovisioninggroup)：修改弹性供应组的配置。
- [DescribeAutoProvisioningGroups](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describeautoprovisioninggroups)：回读弹性供应组状态和资源视图。
- [DescribeAutoProvisioningGroupHistory](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describeautoprovisioninggrouphistory)：查询弹性供应组的调度任务信息。

注意事项：涉及目标容量、启动配置或调度策略的异步变更时，默认等待供应组配置和调度状态稳定，并回读资源视图。指定 `--with-history` 时附带回读调度历史。

## `ecctl ecs auto-provisioning-group delete`

调用 API：

- [DeleteAutoProvisioningGroup](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-deleteautoprovisioninggroup)：删除弹性供应组。

## `ecctl ecs auto-provisioning-group get`

调用 API：

- [DescribeAutoProvisioningGroups](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describeautoprovisioninggroups)：查询弹性供应组。
- [DescribeAutoProvisioningGroupHistory](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describeautoprovisioninggrouphistory)：查询弹性供应组的调度任务信息。
- [DescribeAutoProvisioningGroupInstances](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describeautoprovisioninggroupinstances)：查询弹性供应组内的实例。

注意事项：该查询复用列表 API，通过资源标识或过滤条件收敛到单个资源。 指定 `--with-history` 时附带查询调度历史。 指定 `--with-instances` 时附带查询供应出的实例。

## `ecctl ecs auto-provisioning-group list`

调用 API：

- [DescribeAutoProvisioningGroups](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describeautoprovisioninggroups)：查询弹性供应组。

# ecs deployment-set

资源：部署集

优先级：P2

别名：`ds`

本文件只描述 `ecctl ecs deployment-set` 的 interface 级命令设计：每个操作命令对应哪些 ECS API，不展开 flag、参数结构和输出结构。

## `ecctl ecs deployment-set create`

调用 API：

- [CreateDeploymentSet](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-createdeploymentset)：在指定的地域内创建部署集。

## `ecctl ecs deployment-set update`

调用 API：

- [ModifyDeploymentSetAttribute](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-modifydeploymentsetattribute)：修改部署集的名称和描述信息。
- [DescribeDeploymentSets](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describedeploymentsets)：回读部署集属性。

注意事项：部署集修改后默认回读资源视图，确认变更结果可见。

## `ecctl ecs deployment-set delete`

调用 API：

- [DeleteDeploymentSet](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-deletedeploymentset)：删除部署集。

## `ecctl ecs deployment-set get`

调用 API：

- [DescribeDeploymentSets](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describedeploymentsets)：查询部署集的属性。
- [DescribeDeploymentSetSupportedInstanceTypeFamily](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describedeploymentsetsupportedinstancetypefamily)：查询支持部署集的实例规格族。

注意事项：该查询复用列表 API，通过资源标识或过滤条件收敛到单个资源。 指定 `--with-supported-instance-type-family` 时附带查询支持的实例规格族。

## `ecctl ecs deployment-set list`

调用 API：

- [DescribeDeploymentSets](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describedeploymentsets)：查询部署集的属性。

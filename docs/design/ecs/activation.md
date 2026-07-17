# ecs activation

资源：云助手托管实例注册码

优先级：P3

本文件只描述 `ecctl ecs activation` 的 interface 级命令设计：每个操作命令对应哪些 ECS API，不展开 flag、参数结构和输出结构。

## `ecctl ecs activation create`

调用 API：

- [CreateActivation](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-createactivation)：创建一个激活码。

## `ecctl ecs activation update`

调用 API：

- [DisableActivation](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-disableactivation)：手动禁用指定的激活码。
- [DescribeActivations](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describeactivations)：回读激活码状态。

注意事项：指定 `--disable` 时调用 `DisableActivation`。禁用后默认回读激活码状态，确认禁用结果可见。

## `ecctl ecs activation delete`

调用 API：

- [DeleteActivation](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-deleteactivation)：删除一个未被使用的激活码。

## `ecctl ecs activation get`

调用 API：

- [DescribeActivations](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describeactivations)：查询激活码的使用情况。

注意事项：该查询复用列表 API，通过资源标识或过滤条件收敛到单个资源。

## `ecctl ecs activation list`

调用 API：

- [DescribeActivations](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describeactivations)：查询激活码的使用情况。

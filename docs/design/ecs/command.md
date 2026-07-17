# ecs command

资源：云助手命令模板

优先级：P1

本文件只描述 `ecctl ecs command` 的 interface 级命令设计：每个操作命令对应哪些 ECS API，不展开 flag、参数结构和输出结构。

命令模板是云助手的可复用命令资源；云助手执行记录不单独设计 `invocation` 顶层资源命令，相关查询、停止和属性修改放在 `command` 下。临时执行命令的 RunCommand 归 instance exec。

## `ecctl ecs command create`

调用 API：

- [CreateCommand](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-createcommand)：创建云助手命令。

## `ecctl ecs command update`

调用 API：

- [ModifyCommand](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-modifycommand)：修改云助手命令。
- [ModifyInvocationAttribute](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-modifyinvocationattribute)：修改云助手命令的执行属性。
- [DescribeCommands](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describecommands)：回读云助手命令。
- [DescribeInvocations](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describeinvocations)：回读云助手命令执行记录。

注意事项：默认修改命令模板并回读命令视图。指定 `--invocation-id` 时调用 `ModifyInvocationAttribute` 修改执行记录属性，修改后默认回读执行记录；如果修改影响周期执行或定时执行状态，默认等待目标状态并回读执行视图。

## `ecctl ecs command delete`

调用 API：

- [DeleteCommand](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-deletecommand)：删除一条云助手命令。

## `ecctl ecs command get`

调用 API：

- [DescribeCommands](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describecommands)：查询已创建的云助手命令。
- [DescribeInvocations](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describeinvocations)：查询云助手命令的执行信息列表。
- [DescribeInvocationResults](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describeinvocationresults)：查询云助手命令执行结果。

注意事项：默认查询命令模板，该查询复用列表 API，通过资源标识或过滤条件收敛到单个资源。指定 `--invocation-id` 时调用 `DescribeInvocations`；指定 `--with-results` 时附带查询执行结果。

## `ecctl ecs command list`

调用 API：

- [DescribeCommands](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describecommands)：查询已创建的云助手命令。
- [DescribeInvocations](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describeinvocations)：查询云助手命令的执行信息列表。

注意事项：默认查询命令模板列表；指定 `--invocations` 时调用 `DescribeInvocations` 查询执行记录。

分页约束：`DescribeCommands`、`DescribeInvocations` 和 `DescribeInvocationResults` 使用 `MaxResults/NextToken`；分页上限按 50 处理。`list` 暴露 `--next-token/--limit`，默认 `--limit 50`。

## `ecctl ecs command invoke`

调用 API：

- [InvokeCommand](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-invokecommand)：执行云助手命令。
- [DescribeInvocationResults](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describeinvocationresults)：查询云助手命令执行结果。

注意事项：`InvokeCommand` 是异步执行接口。`command invoke` 默认应等待执行完成并调用 `DescribeInvocationResults` 回读结果，输出包含 `actions[]` 和执行结果；指定 `--no-wait` 时只返回触发执行得到的执行标识。

## `ecctl ecs command stop`

调用 API：

- [StopInvocation](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-stopinvocation)：停止执行云助手命令。
- [DescribeInvocations](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describeinvocations)：查询停止后的执行状态。
- [DescribeInvocationResults](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describeinvocationresults)：查询停止后的执行结果。

注意事项：停止执行记录是命令执行域动作，不单独设计 `invocation stop`。停止执行是异步操作，默认等待执行进入停止或终止状态并回读执行记录。

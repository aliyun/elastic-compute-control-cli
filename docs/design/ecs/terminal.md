# ecs terminal

资源：云助手终端会话

优先级：P3

本文件只描述 `ecctl ecs terminal` 的 interface 级命令设计：每个操作命令对应哪些 ECS API，不展开完整 flag、参数结构和输出结构。

## `ecctl ecs terminal list`

调用 API：

- [DescribeTerminalSessions](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describeterminalsessions)：查询终端会话历史记录。

## `ecctl ecs terminal start`

调用 API：

- [StartTerminalSession](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-startterminalsession)：开始终端会话。

注意事项：不在 `instance` 资源下提供终端会话子命令。

## `ecctl ecs terminal end`

调用 API：

- [EndTerminalSession](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-endterminalsession)：关闭终端会话。

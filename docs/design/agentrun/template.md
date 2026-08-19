# agentrun template

资源：AgentRun 沙箱模板

优先级：P1

本文件描述 `ecctl agentrun template` 的 interface 级命令设计。AgentRun `2025-09-10` 使用 ROA 风格；模板名称是 get、update、delete 及 MCP 管理接口的路径标识，服务端分配的 `templateId` 作为只读字段返回。

## `ecctl agentrun template create`

调用 API：

- [CreateTemplate](https://help.aliyun.com/zh/agentrun/api-agentrun-2025-09-10-createtemplate)：创建沙箱模板。
- [GetTemplate](https://help.aliyun.com/zh/agentrun/api-agentrun-2025-09-10-gettemplate)：默认等待模板进入 `READY` 并回读最终配置。

注意事项：CPU、内存、模板类型和网络配置按官方 SDK 的必填约束暴露；容器、网络、凭证、日志、ARMS、NAS、环境变量和模板专属配置使用 JSON 或 `@file` 输入。OSS 每个挂载配置使用一个 JSON 对象或 `@file`，通过重复 `--oss-configuration` 传入多项。`--no-wait` 只返回已受理的模板名称，不声明模板已经可用。

## `ecctl agentrun template update <template-name>`

调用 API：

- [UpdateTemplate](https://help.aliyun.com/zh/agentrun/api-agentrun-2025-09-10-updatetemplate)：更新模板配置。
- [GetTemplate](https://help.aliyun.com/zh/agentrun/api-agentrun-2025-09-10-gettemplate)：默认等待模板重新进入 `READY` 并回读。

注意事项：更新请求使用 API 的 `clientToken` 幂等参数；CLI 自动生成令牌。至少需要一个实际更新字段或 `--api-param`。

## `ecctl agentrun template delete <template-name>`

调用 API：

- [DeleteTemplate](https://help.aliyun.com/zh/agentrun/api-agentrun-2025-09-10-deletetemplate)：删除模板。
- [ListTemplates](https://help.aliyun.com/zh/agentrun/api-agentrun-2025-09-10-listtemplates)：默认按模板名称确认资源已不可见。

注意事项：删除后不能再用该模板创建沙箱。`--no-wait` 仅表示删除请求已受理。

## `ecctl agentrun template get <template-name>`

调用 API：

- [GetTemplate](https://help.aliyun.com/zh/agentrun/api-agentrun-2025-09-10-gettemplate)：获取模板配置、构建状态和 MCP 状态。

## `ecctl agentrun template list`

调用 API：

- [ListTemplates](https://help.aliyun.com/zh/agentrun/api-agentrun-2025-09-10-listtemplates)：分页列出模板，并支持名称、类型、状态和工作空间过滤。

注意事项：该接口使用 `pageNumber` / `pageSize` 分页，因此命令暴露 `--page` / `--limit`，不使用 next-token。

## `ecctl agentrun template enable <template-name>`

调用 API：

- [ActivateTemplateMCP](https://help.aliyun.com/zh/agentrun/api-agentrun-2025-09-10-activatetemplatemcp)：启用模板 MCP 服务并配置传输协议和工具列表；工具列表使用 JSON 数组，例如 `--enabled-tools '["execute_code"]'`。
- [GetTemplate](https://help.aliyun.com/zh/agentrun/api-agentrun-2025-09-10-gettemplate)：回读 MCP 当前状态和访问端点。

注意事项：官方 API 使用 `PATCH`。API 文档没有给出完整 MCP 终态枚举，因此命令只做一次真实回读，不把 `CREATING` 误报为已经就绪。

## `ecctl agentrun template disable <template-name>`

调用 API：

- [StopTemplateMCP](https://help.aliyun.com/zh/agentrun/api-agentrun-2025-09-10-stoptemplatemcp)：停止模板 MCP 服务并删除关联 MCP 资源。
- [GetTemplate](https://help.aliyun.com/zh/agentrun/api-agentrun-2025-09-10-gettemplate)：回读 MCP 当前状态。

注意事项：该接口同样使用 `PATCH`。如果服务端明确返回 MCP 仍处于 `CREATING`、尚不能停止，命令会在 30 秒有界窗口内重试；其他冲突或失败立即返回。命令最终回读服务端结果，不推断未公开的终态名称。

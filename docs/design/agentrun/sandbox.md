# agentrun sandbox

资源：AgentRun 沙箱实例

优先级：P1

本文件描述 `ecctl agentrun sandbox` 的 interface 级命令设计。沙箱由模板创建，以 `sandboxId` 作为位置参数；停止后进入不可恢复的终止状态。

## `ecctl agentrun sandbox create`

调用 API：

- [CreateSandbox](https://help.aliyun.com/zh/agentrun/api-agentrun-2025-09-10-createsandbox)：基于模板创建沙箱。
- [GetSandbox](https://help.aliyun.com/zh/agentrun/api-agentrun-2025-09-10-getsandbox)：默认等待沙箱进入 `READY` 并回读实例信息。

注意事项：模板名称必填；NAS、OSS 和 PolarFS 挂载配置使用 JSON 或 `@file`。可选 `--id` 透传 API 的指定沙箱 ID 能力。`--no-wait` 只返回创建响应中的 ID。

## `ecctl agentrun sandbox delete <sandbox-id>`

调用 API：

- [DeleteSandbox](https://help.aliyun.com/zh/agentrun/api-agentrun-2025-09-10-deletesandbox)：删除沙箱实例。
- [ListSandboxes](https://help.aliyun.com/zh/agentrun/api-agentrun-2025-09-10-listsandboxes)：默认按沙箱 ID 确认资源已不可见。

## `ecctl agentrun sandbox get <sandbox-id>`

调用 API：

- [GetSandbox](https://help.aliyun.com/zh/agentrun/api-agentrun-2025-09-10-getsandbox)：获取沙箱状态、模板关联、时间、ARN 和元数据。

## `ecctl agentrun sandbox list`

调用 API：

- [ListSandboxes](https://help.aliyun.com/zh/agentrun/api-agentrun-2025-09-10-listsandboxes)：按 ID、模板名称、模板类型或状态过滤沙箱。

注意事项：该接口使用 `maxResults` / `nextToken` 分页，因此命令暴露相邻的 `--limit` / `--next-token`，不提供页码。

## `ecctl agentrun sandbox stop <sandbox-id>`

调用 API：

- [StopSandbox](https://help.aliyun.com/zh/agentrun/api-agentrun-2025-09-10-stopsandbox)：停止沙箱。
- [GetSandbox](https://help.aliyun.com/zh/agentrun/api-agentrun-2025-09-10-getsandbox)：默认等待 `TERMINATED` 并回读。

注意事项：停止后沙箱不可恢复。

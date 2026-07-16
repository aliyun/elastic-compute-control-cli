# lingjun node

资源：灵骏节点

优先级：P1

本文件只描述 `ecctl lingjun node` 的 interface 级命令设计：每个操作命令对应哪些 eflo-controller API，不展开完整 flag、参数结构和输出结构；仅在多 API 分流时标明必要的特殊开关。

设计目标：覆盖灵骏节点的查询、释放、重启、停止，以及远程执行命令和文件分发。节点是灵骏集群中的计算单元，`list` 通过 `--free` 和 `--hyper` 两个正交 flag 组合切换视图（至少指定一个）。`exec` 和 `sendfile` 提供批量远程运维能力，在多个节点上执行 shell 命令或分发文件。

特殊开关命名是 interface 级建议，用来说明何时触发额外 API；最终字段名和参数细节在资源 spec 中定义。

## `ecctl lingjun node delete`

调用 API：

- 默认调用 [DeleteNode](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-controller-2022-12-15-deletenode)：释放按量付费节点。
- 指定 `--hyper` 时调用 [DeleteHyperNode](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-controller-2022-12-15-deletehypernode)：释放超节点（HyperNode）。

注意事项：`delete` 用于释放按量付费节点或超节点，释放后资源不可恢复。包年包月节点不支持通过此命令释放。

形态：`ecctl lingjun node delete <node-id>... [--hyper]`

## `ecctl lingjun node get`

调用 API：

- [DescribeNode](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-controller-2022-12-15-describenode)：查询单个节点详情，包括节点状态、所属集群、机型、网络信息等。

注意事项：`get` 接收单个节点 ID（位置参数），用于详情查看；批量节点列表请使用 `list`。

形态：`ecctl lingjun node get <node-id>`

## `ecctl lingjun node list`

调用 API：

- 指定 `--free` 时调用 [ListFreeNodes](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-controller-2022-12-15-listfreenodes)：列出未分配给集群的空闲节点。
- 指定 `--hyper` 时调用 [ListHyperNodes](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-controller-2022-12-15-listhypernodes)：列出所有超节点（HyperNode）。
- 指定 `--free --hyper` 时调用 [ListFreeHyperNodes](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-controller-2022-12-15-listfreehypernodes)：列出未分配的空闲超节点。

注意事项：`--free` 和 `--hyper` 是两个正交 flag，至少须指定一个（没有"列出全部普通节点"的 API）。集群内节点列表通过 `cluster get --with-nodes` 查询，不在此命令承载。DescribeNode 不在此命令承载，单节点详情请走 `get`。

分页约束：ListFreeNodes / ListHyperNodes / ListFreeHyperNodes 使用 NextToken/MaxResults 分页，默认 `--limit 100`，最大值以官方 API 文档为准。

形态：`ecctl lingjun node list --free`、`ecctl lingjun node list --hyper`、`ecctl lingjun node list --free --hyper`

## `ecctl lingjun node reboot`

调用 API：

- [RebootNodes](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-controller-2022-12-15-rebootnodes)：批量重启节点。
- [DescribeTask](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-controller-2022-12-15-describetask)：轮询重启任务状态。

注意事项：`RebootNodes` 是异步操作，支持批量重启多个节点，默认等待任务执行成功。

形态：`ecctl lingjun node reboot <node-id>... [--no-wait]`

## `ecctl lingjun node stop`

调用 API：

- [StopNodes](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-controller-2022-12-15-stopnodes)：批量停止节点。
- [DescribeTask](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-controller-2022-12-15-describetask)：轮询停止任务状态。

注意事项：`StopNodes` 是异步操作，默认等待任务执行成功。停止后节点进入关机状态，不再产生计算费用（按量付费场景）。

形态：`ecctl lingjun node stop <node-id>... [--no-wait]`

## `ecctl lingjun node exec`

调用 API：

- [RunCommand](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-controller-2022-12-15-runcommand)：在指定节点上执行 shell 命令。
- [DescribeInvocations](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-controller-2022-12-15-describeinvocations)：轮询命令执行状态和结果。

注意事项：`exec` 支持在一个或多个节点上批量执行 shell 命令，是异步操作，默认等待命令执行完成并输出执行结果。

形态：`ecctl lingjun node exec <node-id>... --command "<shell>" [--no-wait] [--timeout <duration>]`

## `ecctl lingjun node sendfile`

调用 API：

- [SendFile](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-controller-2022-12-15-sendfile)：向指定节点发送文件。
- [DescribeSendFileResults](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-controller-2022-12-15-describesendfileresults)：轮询文件发送状态和结果。

注意事项：`sendfile` 支持向一个或多个节点批量分发文件，是异步操作，默认等待文件发送完成并输出分发结果。`--target` 和 `--target-dir` 二选一，指定目标目录。

形态：`ecctl lingjun node sendfile <node-id>... --content <text> --name <filename> --target <dir> [--no-wait] [--timeout <duration>]`

## 暂不进入主命令面的 API

以下 API 已调研，但不作为 `ecctl lingjun node` 的 80% 主命令能力；需要时可先通过原始 OpenAPI 兜底。

- [StopInvocation](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-controller-2022-12-15-stopinvocation)：中止执行中的命令，属于异常态处理；需要时走 `ecctl aliyun eflo-controller StopInvocation`。
- [ReimageNodes](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-controller-2022-12-15-reimagenodes)：重装系统属于高危操作，需要时走 `ecctl aliyun eflo-controller ReimageNodes`。
- [CreateDiagnosticTask](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-controller-2022-12-15-creatediagnostictask) / [DescribeDiagnosticResult](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-controller-2022-12-15-describediagnosticresult) / [ListDiagnosticResults](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-controller-2022-12-15-listdiagnosticresults)：诊断任务属于排障场景，低频使用；需要时走原始 OpenAPI。
- [CreateSession](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-controller-2022-12-15-createsession) / [CloseSession](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-controller-2022-12-15-closesession)：会话管理为内部实现，不独立暴露。
- [ListMachineNetworkInfo](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-controller-2022-12-15-listmachinenetworkinfo)：机器网络信息查询为排障辅助；需要时走原始 OpenAPI。
- [ChangeNodeTypes](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-controller-2022-12-15-changenodetypes)：节点规格变更（resize）属于低频运维操作；需要时走原始 OpenAPI。
- [ReportNodesStatus](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-controller-2022-12-15-reportnodesstatus)：节点状态上报为内部接口，不在 CLI 暴露。
- [ListSyslogs](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-controller-2022-12-15-listsyslogs)：系统日志查询为排障辅助；需要时走原始 OpenAPI。
- [DescribeHyperNode](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-controller-2022-12-15-describehypernode) / [GetHyperNode](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-controller-2022-12-15-gethypernode)：超节点详情已通过 `ecctl lingjun cluster get --with-hyper-nodes` 覆盖；ListHyperNodes 已纳入 `node list --hyper`。
- [DescribeNodeType](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-controller-2022-12-15-describenodetype)：节点规格详情为辅助查询，与 ListMachineTypes 同属创建前辅助能力，需要时走原始 OpenAPI。

# lingjun vsc

资源：灵骏虚拟串行卡

优先级：P2

本文件只描述 `ecctl lingjun vsc` 的 interface 级命令设计：每个操作命令对应哪些 eflo-controller API，不展开完整 flag、参数结构和输出结构；仅在多 API 分流时标明必要的特殊开关。

设计目标：覆盖灵骏节点 Vsc（virtual serial card）的全生命周期。Vsc 是挂在节点上的子对象，用于节点带外通道/串口能力扩展，支持 `primary` 和 `standard` 两种类型；`create` 必须指定所属节点。Vsc 没有 update 接口，章节顺序为 create → delete → get → list。

## `ecctl lingjun vsc create`

调用 API：

- [CreateVsc](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-controller-2022-12-15-createvsc)：在指定节点上创建 Vsc。
- [DescribeVsc](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-controller-2022-12-15-describevsc)：回读 Vsc 视图。

注意事项：`--node` 必填，指定 Vsc 所属节点；`--type` 可选，取值 `primary` 或 `standard`，默认行为以官方 API 文档为准。CreateVsc 为同步创建，创建成功后回读 Vsc 视图。
形态：`ecctl lingjun vsc create --node <node-id> [--type primary|standard] [--name <name>]`

## `ecctl lingjun vsc delete`

调用 API：

- [DeleteVsc](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-controller-2022-12-15-deletevsc)：删除指定 Vsc。

注意事项：删除以 Vsc ID 为唯一定位；删除后该节点上对应的带外/串口通道随之回收。
形态：`ecctl lingjun vsc delete <vsc-id>`

## `ecctl lingjun vsc get`

调用 API：

- [DescribeVsc](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-controller-2022-12-15-describevsc)：查询单个 Vsc 详情。

注意事项：仅返回 Vsc 基础属性视图，所属节点信息以 API 返回字段为准。
形态：`ecctl lingjun vsc get <vsc-id>`

## `ecctl lingjun vsc list`

调用 API：

- [ListVscs](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-controller-2022-12-15-listvscs)：查询 Vsc 列表，支持按节点过滤。

注意事项：`--filter node=` 可选，按所属节点过滤；不指定时返回当前账号可见的全部 Vsc。
分页约束：ListVscs 使用 NextToken/MaxResults 分页，默认 `--limit 100`，最大值以官方 API 文档为准。
形态：`ecctl lingjun vsc list [--filter node=<node-id>] [--limit 100]`

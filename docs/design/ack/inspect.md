# ack inspect

资源：集群巡检

优先级：P1

本文件只描述 `ecctl ack inspect` 的 interface 级命令设计：每个操作命令对应哪些 ACK API，不展开完整 flag、参数结构和输出结构；仅在多 API 分流时标明必要的特殊开关。

设计目标：覆盖集群巡检的周期性配置、按需触发巡检与历史报告查询。`inspect` 资源下区分两个子对象：

- **巡检配置（config）**：集群级周期性巡检的开关、周期、范围等参数；只用词表内动作 `config update / config get / config delete`，其中 `update` 是 upsert 语义。
- **巡检报告（report）**：每次巡检产生的结果快照；只用词表内动作 `report create / report list / report get`，其中 `create` 触发一次新巡检并默认等待回读。

约束：

- `config update` 是 upsert 语义，CLI 内部先调用 `GetClusterInspectConfig` 判断集群是否已有配置，再分流到 `CreateClusterInspectConfig` 或 `UpdateClusterInspectConfig`，外部命令面只暴露一个 `config update`，不单独设计 `config create` 与 `config set`。
- `report create` 是异步操作，默认等待巡检任务完成并回读最新报告；高级用户可通过 `--no-wait` 拿到 task_id 后用 `ack task ...` 控制。不再独立设计 `inspect run`。

特殊开关命名是 interface 级建议，用来说明何时触发额外 API；最终字段名和参数细节在资源 spec 中定义。

## `ecctl ack inspect config update`

调用 API：

- 集群无巡检配置时调用 [CreateClusterInspectConfig](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-createclusterinspectconfig)：创建集群巡检配置。
- 集群已有巡检配置时调用 [UpdateClusterInspectConfig](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-updateclusterinspectconfig)：更新集群巡检配置。
- [GetClusterInspectConfig](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-getclusterinspectconfig)：判断配置是否存在并回读最终配置视图。

注意事项：`config update` 是 upsert，CLI 内部按是否已存在分流到 `CreateClusterInspectConfig` 或 `UpdateClusterInspectConfig`，不单独设计 `config create` 与 `config set`。单例配置统一用 `update` 表达期望状态，符合 cli-design-rules 的"开关用 flag、单例配置归入 update"规则。变更完成后回读配置视图。

## `ecctl ack inspect config delete`

调用 API：

- [DeleteClusterInspectConfig](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-deleteclusterinspectconfig)：删除集群巡检配置。

注意事项：删除配置只关闭周期性巡检，不影响历史报告（历史报告仍可通过 `report list / report get` 查询）。

## `ecctl ack inspect config get`

调用 API：

- [GetClusterInspectConfig](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-getclusterinspectconfig)：查询集群巡检配置。

注意事项：集群无巡检配置时返回空视图，不视为错误。

## `ecctl ack inspect report create`

调用 API：

- [RunClusterInspect](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-runclusterinspect)：触发一次集群巡检并生成新报告。
- [ListClusterInspectReports](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-listclusterinspectreports)：等待巡检完成后回读最新报告 ID。
- [GetClusterInspectReportDetail](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-getclusterinspectreportdetail)：回读最新报告详情。

注意事项：`report create` 表达"创建一份新巡检报告"——触发一次巡检并把结果作为新报告资源沉淀，因此使用 `create` 而不是 `inspect run`，与 `ecs diagnostic report create` / `ack check create` / `ack vuls create` 等异步任务创建语义对齐。`RunClusterInspect` 是异步触发接口，默认等待巡检任务进入终态并回读最新报告；指定 `--no-wait` 时只返回 `task_id`，由 `ack task` 命令面跟进或调用方再次 `inspect report list` 拉取。

## `ecctl ack inspect report get`

调用 API：

- [GetClusterInspectReportDetail](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-getclusterinspectreportdetail)：查询单份巡检报告详情。

## `ecctl ack inspect report list`

调用 API：

- [ListClusterInspectReports](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-listclusterinspectreports)：列出该集群历史巡检报告。

## 暂不进入主命令面的 API

当前 `ecctl ack inspect` 已覆盖巡检场景的全部已知 API，暂无需要兜底的项。

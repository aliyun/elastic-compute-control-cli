# ecs diagnostic

资源：资源诊断

优先级：P3

别名：`diag`

本文件只描述 `ecctl ecs diagnostic` 的 interface 级命令设计：每个操作命令对应哪些 ECS API，不展开 flag、参数结构和输出结构。

`diagnostic` 是资源诊断能力域，承载诊断指标目录、诊断指标集合和诊断报告。`diagnostic-metric-set`、`diagnostic-report` 不再作为 ECS 顶层资源命令。

## `ecctl ecs diagnostic metric-set create`

调用 API：

- [CreateDiagnosticMetricSet](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-creatediagnosticmetricset)：创建资源诊断指标集合。

## `ecctl ecs diagnostic report create`

调用 API：

- [CreateDiagnosticReport](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-creatediagnosticreport)：创建资源诊断报告。
- [DescribeDiagnosticReportAttributes](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describediagnosticreportattributes)：查询资源诊断报告详情。

注意事项：创建诊断报告是异步操作，默认等待报告生成完成并回读报告详情。

## `ecctl ecs diagnostic metric-set update`

调用 API：

- [ModifyDiagnosticMetricSet](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-modifydiagnosticmetricset)：修改资源诊断指标集合。
- [DescribeDiagnosticMetricSets](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describediagnosticmetricsets)：回读资源诊断集合列表。

注意事项：诊断指标集合修改后默认回读资源视图，确认变更结果可见。

## `ecctl ecs diagnostic metric-set delete`

调用 API：

- [DeleteDiagnosticMetricSets](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-deletediagnosticmetricsets)：删除资源诊断指标集合。

## `ecctl ecs diagnostic report delete`

调用 API：

- [DeleteDiagnosticReports](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-deletediagnosticreports)：删除资源诊断报告。

## `ecctl ecs diagnostic metric-set get`

调用 API：

- [DescribeDiagnosticMetricSets](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describediagnosticmetricsets)：查询资源诊断集合列表。

注意事项：该查询复用列表 API，通过资源标识或过滤条件收敛到单个指标集合。

## `ecctl ecs diagnostic report get`

调用 API：

- [DescribeDiagnosticReportAttributes](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describediagnosticreportattributes)：查询资源诊断报告详情。

## `ecctl ecs diagnostic metric list`

调用 API：

- [DescribeDiagnosticMetrics](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describediagnosticmetrics)：查询诊断指标列表。

## `ecctl ecs diagnostic metric-set list`

调用 API：

- [DescribeDiagnosticMetricSets](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describediagnosticmetricsets)：查询资源诊断集合列表。

## `ecctl ecs diagnostic report list`

调用 API：

- [DescribeDiagnosticReports](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describediagnosticreports)：查询资源诊断报告列表。

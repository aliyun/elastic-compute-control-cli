# ecs maintenance-window

资源：计划运维窗口

优先级：P2

别名：`mw`

本文件只描述 `ecctl ecs maintenance-window` 的 interface 级命令设计：每个操作命令对应哪些 ECS API，不展开 flag、参数结构和输出结构。

## `ecctl ecs maintenance-window create`

调用 API：

- [CreatePlanMaintenanceWindow](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-createplanmaintenancewindow)：创建运维窗口。

## `ecctl ecs maintenance-window update`

调用 API：

- [ModifyPlanMaintenanceWindow](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-modifyplanmaintenancewindow)：修改运维窗口。
- [DescribePlanMaintenanceWindows](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describeplanmaintenancewindows)：回读运维窗口。

注意事项：运维窗口修改后默认回读资源视图，确认变更结果可见。

## `ecctl ecs maintenance-window delete`

调用 API：

- [DeletePlanMaintenanceWindow](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-deleteplanmaintenancewindow)：删除运维窗口。

## `ecctl ecs maintenance-window get`

调用 API：

- [DescribePlanMaintenanceWindows](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describeplanmaintenancewindows)：查询运维窗口。

注意事项：该查询复用列表 API，通过资源标识或过滤条件收敛到单个资源。

## `ecctl ecs maintenance-window list`

调用 API：

- [DescribePlanMaintenanceWindows](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describeplanmaintenancewindows)：查询运维窗口。

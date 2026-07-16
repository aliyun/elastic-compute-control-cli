# ecs event

资源：系统事件

优先级：P2

本文件只描述 `ecctl ecs event` 的 interface 级命令设计：每个操作命令对应哪些 ECS API，不展开 flag、参数结构和输出结构。

## `ecctl ecs event create`

调用 API：

- [CreateSimulatedSystemEvents](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-createsimulatedsystemevents)：为实例预约模拟系统事件。

## `ecctl ecs event update`

调用 API：

- [AcceptInquiredSystemEvent](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-acceptinquiredsystemevent)：接受并授权执行系统事件操作。
- [DescribeInstanceHistoryEvents](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describeinstancehistoryevents)：回读系统事件状态。

注意事项：接受系统事件会触发事件状态流转，默认等待事件进入已授权或执行中等目标状态并回读事件视图。

## `ecctl ecs event delete`

调用 API：

- [CancelSimulatedSystemEvents](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-cancelsimulatedsystemevents)：取消模拟系统事件。

## `ecctl ecs event list`

调用 API：

- [DescribeInstanceHistoryEvents](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describeinstancehistoryevents)：查询指定实例系统事件信息。
- [DescribeInstancesFullStatus](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describeinstancesfullstatus)：查询实例的全部状态信息。
- [DescribeDisksFullStatus](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describedisksfullstatus)：查询一块或多块块存储的全部状态信息。

注意事项：默认按实例系统事件查询；指定 `--view instance-full-status` 时调用 `DescribeInstancesFullStatus`，指定 `--view disk-full-status` 时调用 `DescribeDisksFullStatus`。

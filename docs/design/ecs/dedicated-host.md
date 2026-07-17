# ecs dedicated-host

资源：专有宿主机

优先级：P3

别名：`ddh`

本文件只描述 `ecctl ecs dedicated-host` 的 interface 级命令设计：每个操作命令对应哪些 ECS API，不展开 flag、参数结构和输出结构。

## `ecctl ecs dedicated-host create`

调用 API：

- [AllocateDedicatedHosts](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-allocatededicatedhosts)：创建一台或多台按量付费或者包年包月专有宿主机。
- [DescribeDedicatedHosts](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describededicatedhosts)：回读专有宿主机状态和资源视图。

注意事项：创建是异步操作，默认等待专有宿主机进入可编排状态并回读资源视图。

## `ecctl ecs dedicated-host update`

调用 API：

- [ModifyDedicatedHostAttribute](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-modifydedicatedhostattribute)：修改专有宿主机部分信息。
- [ModifyDedicatedHostAutoReleaseTime](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-modifydedicatedhostautoreleasetime)：为专有宿主机设定自动释放时间。
- [ModifyDedicatedHostAutoRenewAttribute](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-modifydedicatedhostautorenewattribute)：为专有宿主机设置或取消自动续费。
- [ModifyDedicatedHostsChargeType](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-modifydedicatedhostschargetype)：修改专有宿主机的付费类型。
- [DescribeDedicatedHosts](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describededicatedhosts)：回读专有宿主机状态和资源视图。
- [DescribeDedicatedHostAutoRenew](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describededicatedhostautorenew)：回读专有宿主机自动续费状态。

注意事项：指定 `--auto-release-time` 时调用 `ModifyDedicatedHostAutoReleaseTime`。指定 `--auto-renew` 时调用 `ModifyDedicatedHostAutoRenewAttribute`。指定 `--charge-type` 时调用 `ModifyDedicatedHostsChargeType`。涉及付费类型或自动释放等状态变更时，默认等待目标状态并回读宿主机视图；自动续费变更默认回读自动续费状态。

## `ecctl ecs dedicated-host delete`

调用 API：

- [ReleaseDedicatedHost](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-releasededicatedhost)：释放专有宿主机。
- [DescribeDedicatedHosts](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describededicatedhosts)：确认专有宿主机释放完成。

注意事项：释放是异步操作时，默认等待专有宿主机不可见或进入释放终态。

## `ecctl ecs dedicated-host get`

调用 API：

- [DescribeDedicatedHosts](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describededicatedhosts)：查询专有宿主机详细信息。
- [DescribeDedicatedHostAutoRenew](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describededicatedhostautorenew)：查询专有宿主机自动续费状态。

注意事项：该查询复用列表 API，通过资源标识或过滤条件收敛到单个资源。 指定 `--with-auto-renew` 时附带查询自动续费状态。

## `ecctl ecs dedicated-host list`

调用 API：

- [DescribeDedicatedHosts](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describededicatedhosts)：查询专有宿主机详细信息。
- [DescribeDedicatedHostTypes](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describededicatedhosttypes)：查询专有宿主机规格详细参数。

注意事项：默认查询专有宿主机列表；指定 `--types` 时调用 `DescribeDedicatedHostTypes` 查询专有宿主机规格。

## `ecctl ecs dedicated-host redeploy`

调用 API：

- [RedeployDedicatedHost](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-redeploydedicatedhost)：执行专有宿主机的故障迁移。
- [DescribeDedicatedHosts](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describededicatedhosts)：查询故障迁移后的专有宿主机状态。

注意事项：故障迁移是异步操作，默认等待迁移完成并回读专有宿主机视图。

## `ecctl ecs dedicated-host renew`

调用 API：

- [RenewDedicatedHosts](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-renewdedicatedhosts)：续费一台或者多台包年包月专有宿主机。

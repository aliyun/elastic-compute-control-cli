# ecs instance

资源：实例

优先级：P0

本文件只描述 `ecctl ecs instance` 的 interface 级命令设计：每个操作命令对应哪些 ECS API，不展开完整 flag、参数结构和输出结构；仅在多 API 分流时标明必要的特殊开关。

设计目标：优先覆盖 80% 常用实例管理场景。废弃、不推荐、询价、ClassicLink、故障反馈、故障迁移等低频能力不进入主命令面，在后续章节说明。

特殊开关命名是 interface 级建议，用来说明何时触发额外 API；最终字段名和参数细节在资源 spec 中定义。

## `ecctl ecs instance create`

调用 API：

- [RunInstances](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-runinstances)：创建一台或多台实例。
- [DescribeInstances](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describeinstances)：回读实例状态和资源视图。

注意事项：`create` 统一使用 `RunInstances`。创建时指定 securityGroupIds、roleName 或 keyPairName 也由 `RunInstances` 承载，不需要单独的 `sg join`、`ram-role attach` 或 `keypair attach` 命令。`RunInstances` 是异步创建接口，默认等待实例进入可编排状态并回读实例视图。旧创建接口的处理见最后的废弃/不推荐 API 章节。

创建前如需选择可用区、实例规格或系统盘类型组合，先使用 `ecctl call ecs DescribeAvailableResource` 查询库存。`DestinationResource` 用于选择库存维度；不传 `ZoneId` 时查询该地域下所有可用区，查询系统盘库存时 `DestinationResource=SystemDisk` 需要配合 `InstanceType` 使用。`ecs instance create` 不自动替换用户传入的可用区、规格或磁盘类型；遇到库存或磁盘类型不支持错误时，只在结构化错误里返回可执行的 `suggested_action`。

## `ecctl ecs instance update`

调用 API：

- 修改实例名称、描述、主机名等基础属性时调用 [ModifyInstanceAttribute](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-modifyinstanceattribute)：修改实例基础属性。
- 指定自动释放时间时调用 [ModifyInstanceAutoReleaseTime](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-modifyinstanceautoreleasetime)：修改自动释放时间。
- 指定自动续费设置时调用 [ModifyInstanceAutoRenewAttribute](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-modifyinstanceautorenewattribute)：修改实例自动续费配置。
- 指定付费类型变更时调用 [ModifyInstanceChargeType](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-modifyinstancechargetype)：修改实例付费类型。
- 指定时钟配置变更时调用 [ModifyInstanceClockOptions](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-modifyinstanceclockoptions)：修改实例时钟配置。
- 指定维护属性变更时调用 [ModifyInstanceMaintenanceAttributes](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-modifyinstancemaintenanceattributes)：修改实例维护属性。
- 指定元数据访问配置变更时调用 [ModifyInstanceMetadataOptions](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-modifyinstancemetadataoptions)：修改实例元数据选项。
- 指定网络高级选项变更时调用 [ModifyInstanceNetworkOptions](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-modifyinstancenetworkoptions)：修改实例网络高级选项。
- 指定公网带宽调整时调用 [ModifyInstanceNetworkSpec](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-modifyinstancenetworkspec)：修改实例公网带宽或分配公网 IP。
- 指定后付费实例规格变更时调用 [ModifyInstanceSpec](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-modifyinstancespec)：修改实例规格或公网带宽。
- 指定包年包月实例规格变更时调用 [ModifyPrepayInstanceSpec](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-modifyprepayinstancespec)：修改包年包月实例规格。
- 指定 VNC 密码变更时调用 [ModifyInstanceVncPasswd](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-modifyinstancevncpasswd)：修改实例 VNC 密码。
- 指定 VPC 属性变更时调用 [ModifyInstanceVpcAttribute](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-modifyinstancevpcattribute)：修改实例 VPC 属性。
- 指定分配公网 IP 时调用 [AllocatePublicIpAddress](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-allocatepublicipaddress)：为实例分配公网 IP。
- 指定系统盘更换时调用 [ReplaceSystemDisk](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-replacesystemdisk)：更换系统盘（更换操作系统）。
- 指定新的 roleName 时调用 [AttachInstanceRamRole](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-attachinstanceramrole)：为实例绑定 RAM 角色。
- 指定 roleName 为空时调用 [DetachInstanceRamRole](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-detachinstanceramrole)：解绑实例 RAM 角色。
- 指定新的 keyPairName 时调用 [AttachKeyPair](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-attachkeypair)：为实例绑定密钥对。
- 指定 keyPairName 为空时调用 [DetachKeyPair](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-detachkeypair)：为实例解绑密钥对。
- 指定新的 securityGroupIds 且需要加入安全组时调用 [JoinSecurityGroup](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-joinsecuritygroup)：将实例加入安全组。
- 指定新的 securityGroupIds 且需要移出安全组时调用 [LeaveSecurityGroup](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-leavesecuritygroup)：将实例移出安全组。
- 指定资源组归属变更时调用 [JoinResourceGroup](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-joinresourcegroup)：将实例加入资源组。
- 指定标签新增或修改时调用 [TagResources](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-tagresources)：为实例绑定标签。
- 指定标签移除时调用 [UntagResources](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-untagresources)：为实例解绑标签。
- [DescribeInstanceAttribute](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describeinstanceattribute)：回读实例变更后的资源视图。
- [ListTagResources](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-listtagresources)：回读实例标签。

注意事项：规格变更、包年包月规格变更、元数据、维护属性、自动续费、VNC 密码、网络、公网 IP、系统盘更换、RAM role、密钥对、安全组关系、资源组归属和标签关系等都归入 `update`，不再独立设计 `resize`、`metadata update`、`vnc-password update`、`system-disk replace`、`ram-role attach`、`ram-role detach`、`keypair attach`、`keypair detach`、`sg join`、`sg leave`、`resource-group update`、`tag apply`、`tag remove` 等子命令。`ReplaceSystemDisk` 的授权资源还涉及 `disk`、`image`、`instance`；这里按用户目标对象归入实例更新。securityGroupIds 更新按期望集合计算差异后分别调用 `JoinSecurityGroup` 和 `LeaveSecurityGroup`。标签按期望集合计算差异后分别调用 `TagResources` 和 `UntagResources`。涉及实例规格、付费类型、系统盘、网络或关系绑定的异步变更时，默认等待目标状态并回读实例视图；资源组和标签存在最终一致延迟时，默认等待目标关系可见并回读。

## `ecctl ecs instance delete`

调用 API：

- [DeleteInstance](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-deleteinstance)：删除单台实例。
- [DeleteInstances](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-deleteinstances)：批量删除实例。
- [DescribeInstances](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describeinstances)：确认实例删除完成。

注意事项：删除一个实例时调用 `DeleteInstance`；删除多个实例时调用 `DeleteInstances`。删除是异步操作，默认等待实例不可见或进入删除终态。

## `ecctl ecs instance get`

调用 API：

- 默认调用 [DescribeInstanceAttribute](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describeinstanceattribute)：查询单台实例详情。
- 指定 `--with-auto-renew` 时调用 [DescribeInstanceAutoRenewAttribute](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describeinstanceautorenewattribute)：查询实例自动续费配置。
- 指定 `--with-maintenance` 时调用 [DescribeInstanceMaintenanceAttributes](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describeinstancemaintenanceattributes)：查询实例维护属性。
- 指定 `--with-ram-role` 时调用 [DescribeInstanceRamRole](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describeinstanceramrole)：查询实例 RAM 角色。
- 指定 `--with-user-data` 时调用 [DescribeUserData](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describeuserdata)：查询实例 UserData。
- 指定 `--with-vnc-url` 时调用 [DescribeInstanceVncUrl](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describeinstancevncurl)：查询实例 VNC 登录地址。
- 指定 `--with-assistant` 时调用 [DescribeCloudAssistantStatus](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describecloudassistantstatus)：查询实例云助手状态。
- 指定 `--with-plugin-status` 时调用 [ListPluginStatus](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-listpluginstatus)：查询实例插件状态。
- 指定 `--with-tags` 时调用 [ListTagResources](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-listtagresources)：查询实例标签。

注意事项：默认只取实例基础详情；特殊开关用于按需追加查询，避免 `get` 默认触发过多 API。标签是实例关系视图，不在 `ecs` 产品下单独设计 `tag list`；跨产品标签查询由 `ecctl tag resource list` 承载。VNC 地址不单独设计 `vnc-url` 命令，通过 `get --with-vnc-url` 获取。

## `ecctl ecs instance list`

调用 API：

- 默认调用 [DescribeInstances](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describeinstances)：查询实例列表。
- 指定 `--filter status=Running` 时仍调用 [DescribeInstances](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describeinstances)：按实例状态过滤列表。


分页约束：`DescribeInstances` 使用 `MaxResults/NextToken`，`list` 暴露 `--next-token/--limit`。`MaxResults` 最大值为 100，默认 `--limit 100`。

## `ecctl ecs instance start`

调用 API：

- [StartInstance](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-startinstance)：启动单台实例。
- [StartInstances](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-startinstances)：批量启动实例。
- [DescribeInstances](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describeinstances)：查询实例启动后的状态。

注意事项：启动一个实例时调用 `StartInstance`；启动多个实例时调用 `StartInstances`。启动是异步操作，默认等待实例进入运行态并回读实例视图。

## `ecctl ecs instance stop`

调用 API：

- [StopInstance](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-stopinstance)：停止单台实例。
- [StopInstances](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-stopinstances)：批量停止实例。
- [DescribeInstances](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describeinstances)：查询实例停止后的状态。

注意事项：停止一个实例时调用 `StopInstance`；停止多个实例时调用 `StopInstances`。停止是异步操作，默认等待实例进入停止态并回读实例视图。

## `ecctl ecs instance reboot`

调用 API：

- [RebootInstance](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-rebootinstance)：重启单台实例。
- [RebootInstances](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-rebootinstances)：批量重启实例。
- [DescribeInstances](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describeinstances)：查询实例重启后的状态。

注意事项：重启一个实例时调用 `RebootInstance`；重启多个实例时调用 `RebootInstances`。重启是异步操作，默认等待实例回到运行态并回读实例视图。

## `ecctl ecs instance renew`

调用 API：

- [RenewInstance](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-renewinstance)：续费实例。

## `ecctl ecs instance exec`

调用 API：

- [RunCommand](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-runcommand)：在实例上执行临时命令。
- [DescribeInvocations](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describeinvocations)：轮询命令执行状态。
- [DescribeInvocationResults](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describeinvocationresults)：查询云助手命令执行结果。

注意事项：这是云助手 API，但授权资源是 `instance/{#instanceId}`，因此设计为实例动作。`RunCommand` 是异步执行接口，默认等待执行完成并回读执行结果。

## `ecctl ecs instance sendfile`

调用 API：

- [SendFile](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-sendfile)：向实例下发文件。
- 调用 `SendFile` 后自动调用 [DescribeSendFileResults](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describesendfileresults)：查询文件下发结果。

注意事项：这是云助手 API，但授权资源是 `instance/{#instanceId}`，因此设计为实例动作。`SendFile` 是异步下发接口，默认等待下发完成并回读结果；下发结果不单独设计结果查询命令，由 `sendfile` 自动查询。

## `ecctl ecs instance monitor`

调用 API：

- [DescribeInstanceMonitorData](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describeinstancemonitordata)：查询实例监控数据。

## 暂不进入主命令面的 API

以下 API 已调研，但不作为 `ecctl ecs instance` 的 80% 主命令能力；其中部分会由其他资源命令承载，其余需要时可先通过原始 OpenAPI 兜底。

- [DescribeInstanceStatus](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describeinstancestatus)：不单独支持；实例状态通过 `list --filter status=Running` 使用 `DescribeInstances` 表达。
- [DescribeInstanceModificationPrice](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describeinstancemodificationprice)：询价能力，当前先不支持。
- [DescribeBandwidthLimitation](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describebandwidthlimitation)：公网带宽限制查询，偏规格/容量辅助查询，当前先不进入 instance 主命令。
- [ModifyDiskChargeType](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-modifydiskchargetype)：修改云盘计费方式，按 `disk` 资源命令承载，不作为 instance 主命令。
- [InstallCloudAssistant](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-installcloudassistant)：按 `assistant` 资源命令承载。
- [StartTerminalSession](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-startterminalsession)：按 `terminal` 资源命令承载。
- [DescribeTerminalSessions](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describeterminalsessions)：按 `terminal` 资源命令承载。
- [GetInstanceConsoleOutput](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-getinstanceconsoleoutput)：控制台输出属于低频排障能力，当前先不进入 instance 主命令。
- [GetInstanceScreenshot](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-getinstancescreenshot)：实例截屏属于低频排障能力，当前先不进入 instance 主命令。
- [DescribeClassicLinkInstances](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describeclassiclinkinstances)：ClassicLink 查询，属于经典网络兼容场景。
- [AttachClassicLinkVpc](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-attachclassiclinkvpc)：ClassicLink 连接，属于经典网络兼容场景。
- [DetachClassicLinkVpc](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-detachclassiclinkvpc)：ClassicLink 断开，属于经典网络兼容场景。
- [ConvertNatPublicIpToEip](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-convertnatpubliciptoeip)：公网 IP 转 EIP，属于低频迁移场景。
- [ModifyInstanceDeployment](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-modifyinstancedeployment)：修改实例部署相关属性，属于部署/宿主机关系的低频操作。
- [ReActivateInstances](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-reactivateinstances)：重新激活欠费或过期回收实例，属于低频异常状态处理。
- [RedeployInstance](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-redeployinstance)：重新部署实例，属于故障迁移场景。
- [ReportInstancesStatus](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-reportinstancesstatus)：上报实例异常问题，属于低频反馈场景。

## 废弃/不推荐 API

- [CreateInstance](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-createinstance)：官方文档说明该接口已停止迭代更新，建议使用 `RunInstances`。因此 `ecctl ecs instance create` 不使用该 API。

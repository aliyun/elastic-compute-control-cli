# ecs assistant

资源：云助手

优先级：P1

本文件只描述 `ecctl ecs assistant` 的 interface 级命令设计：每个操作命令对应哪些 ECS API，不展开 flag、参数结构和输出结构。

云助手资源承载云助手安装和服务级设置。实例侧执行命令、下发文件仍放在 `instance` 资源下。

## `ecctl ecs assistant update`

调用 API：

- [ModifyCloudAssistantSettings](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-modifycloudassistantsettings)：修改云助手服务配置。
- [DescribeCloudAssistantSettings](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describecloudassistantsettings)：回读云助手服务配置。

注意事项：服务配置修改后默认回读云助手设置，确认变更结果可见。

## `ecctl ecs assistant get`

调用 API：

- [DescribeCloudAssistantSettings](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describecloudassistantsettings)：查询云助手服务配置。

## `ecctl ecs assistant install`

调用 API：

- [InstallCloudAssistant](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-installcloudassistant)：为实例安装云助手 Agent。
- [DescribeCloudAssistantStatus](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describecloudassistantstatus)：查询实例云助手安装状态。

注意事项：不在 `instance` 资源下提供云助手安装子命令。安装是异步操作时，默认等待 `CloudAssistantStatus=true` 并回读安装状态。

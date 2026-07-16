# ecs launch-template

资源：启动模板

优先级：P2

别名：`lt`

本文件只描述 `ecctl ecs launch-template` 的 interface 级命令设计：每个操作命令对应哪些 ECS API，不展开 flag、参数结构和输出结构。

## `ecctl ecs launch-template create`

调用 API：

- [CreateLaunchTemplate](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-createlaunchtemplate)：创建实例启动模板。

## `ecctl ecs launch-template update`

调用 API：

- [CreateLaunchTemplateVersion](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-createlaunchtemplateversion)：在实例启动模板中创建新版本。
- [ModifyLaunchTemplateDefaultVersion](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-modifylaunchtemplatedefaultversion)：切换启动模板的默认版本。
- [DescribeLaunchTemplates](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describelaunchtemplates)：回读启动模板。
- [DescribeLaunchTemplateVersions](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describelaunchtemplateversions)：回读启动模板版本信息。

注意事项：指定 `--create-version` 时调用 `CreateLaunchTemplateVersion`；指定 `--default-version` 时调用 `ModifyLaunchTemplateDefaultVersion`。创建版本或切换默认版本后默认回读模板和版本信息，确认目标版本可见。

## `ecctl ecs launch-template delete`

调用 API：

- [DeleteLaunchTemplate](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-deletelaunchtemplate)：删除实例启动模板。
- [DeleteLaunchTemplateVersion](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-deletelaunchtemplateversion)：删除实例启动模板的版本。

注意事项：默认删除启动模板；指定 `--version` 时调用 `DeleteLaunchTemplateVersion`。

## `ecctl ecs launch-template get`

调用 API：

- [DescribeLaunchTemplates](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describelaunchtemplates)：查询一个或多个可用的实例启动模板。
- [DescribeLaunchTemplateVersions](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describelaunchtemplateversions)：查询实例启动模板的版本信息。

注意事项：默认查询启动模板详情；指定 `--with-versions` 时附带查询版本信息。

## `ecctl ecs launch-template list`

调用 API：

- [DescribeLaunchTemplates](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describelaunchtemplates)：查询一个或多个可用的实例启动模板。

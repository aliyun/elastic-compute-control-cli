# ecs image

资源：镜像

优先级：P0

本文件只描述 `ecctl ecs image` 的 interface 级命令设计：每个操作命令对应哪些 ECS API，不展开 flag、参数结构和输出结构。

## `ecctl ecs image create`

调用 API：

- [CreateImage](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-createimage)：创建自定义镜像。
- [DescribeImages](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describeimages)：回读镜像状态和资源视图。

注意事项：创建镜像是异步操作，默认等待镜像进入可用状态并回读镜像视图。

## `ecctl ecs image update`

调用 API：

- [ModifyImageAttribute](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-modifyimageattribute)：修改自定义镜像属性。
- [ModifyImageSharePermission](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-modifyimagesharepermission)：管理镜像共享权限。
- [DescribeImages](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describeimages)：回读镜像资源。
- [DescribeImageSharePermission](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describeimagesharepermission)：回读镜像共享权限。

注意事项：指定 `--share-add`、`--share-remove` 或 `--launch-permission` 时调用 `ModifyImageSharePermission`。镜像属性修改后默认回读镜像视图；共享权限变更存在传播延迟时，默认等待共享关系可见并回读共享权限。

## `ecctl ecs image delete`

调用 API：

- [DeleteImage](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-deleteimage)：删除自定义镜像。
- [DescribeImages](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describeimages)：确认镜像删除完成。

注意事项：删除是异步操作时，默认等待镜像不可见或进入删除终态。

## `ecctl ecs image get`

调用 API：

- [DescribeImages](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describeimages)：查询镜像资源。
- [DescribeImageSharePermission](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describeimagesharepermission)：查询自定义镜像已经共享的所有用户。
- [DescribeImageFromFamily](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describeimagefromfamily)：查询镜像族系内可用镜像。
- [DescribeImageSupportInstanceTypes](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describeimagesupportinstancetypes)：查询指定镜像支持的实例规格。

注意事项：该查询复用列表 API，通过资源标识或过滤条件收敛到单个资源。指定 `--with-share-permission` 时附带查询镜像共享权限。指定 `--family` 时调用 `DescribeImageFromFamily` 查询最新可用镜像。指定 `--with-supported-instance-types` 时附带查询镜像支持的实例规格。

## `ecctl ecs image list`

调用 API：

- [DescribeImages](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describeimages)：查询镜像资源。

分页约束：`DescribeImages` 的 `PageSize` 最大值为 100；镜像共享权限回读使用的 `DescribeImageSharePermission` 也是 100，`list` 默认 `--limit 100`。

## `ecctl ecs image copy`

调用 API：

- [CopyImage](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-copyimage)：复制一个地域下的自定义镜像到其他地域。
- [CancelCopyImage](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-cancelcopyimage)：取消正在进行中的复制镜像任务。
- [DescribeImages](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describeimages)：查询复制出的镜像状态。

注意事项：复制镜像是异步操作，默认等待目标镜像进入可用状态并回读镜像视图。指定 `--cancel` 时调用 `CancelCopyImage`，并等待复制任务进入取消或终止状态。

## `ecctl ecs image export`

调用 API：

- [ExportImage](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-exportimage)：导出一份自定义镜像到 OSS。
- [DescribeTaskAttribute](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describetaskattribute)：查询导出任务状态。

注意事项：导出镜像是异步任务，默认等待导出任务完成并回读任务结果。

## `ecctl ecs image import`

调用 API：

- [ImportImage](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-importimage)：导入本地镜像文件。
- [DescribeImages](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describeimages)：查询导入后的镜像状态。

注意事项：导入镜像是异步操作，默认等待镜像进入可用状态并回读镜像视图。

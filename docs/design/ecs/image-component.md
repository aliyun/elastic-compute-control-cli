# ecs image-component

资源：镜像组件

优先级：P3

别名：`ic`

本文件只描述 `ecctl ecs image-component` 的 interface 级命令设计：每个操作命令对应哪些 ECS API，不展开 flag、参数结构和输出结构。

## `ecctl ecs image-component create`

调用 API：

- [CreateImageComponent](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-createimagecomponent)：创建镜像组件。

## `ecctl ecs image-component delete`

调用 API：

- [DeleteImageComponent](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-deleteimagecomponent)：删除镜像组件。

## `ecctl ecs image-component get`

调用 API：

- [DescribeImageComponents](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describeimagecomponents)：查询镜像组件的详细信息。

注意事项：该查询复用列表 API，通过资源标识或过滤条件收敛到单个资源。

## `ecctl ecs image-component list`

调用 API：

- [DescribeImageComponents](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describeimagecomponents)：查询镜像组件的详细信息。

# ecs image-pipeline

资源：镜像模板/流水线

优先级：P3

别名：`ip`

本文件只描述 `ecctl ecs image-pipeline` 的 interface 级命令设计：每个操作命令对应哪些 ECS API，不展开 flag、参数结构和输出结构。

## `ecctl ecs image-pipeline create`

调用 API：

- [CreateImagePipeline](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-createimagepipeline)：创建镜像构建模板。

## `ecctl ecs image-pipeline delete`

调用 API：

- [DeleteImagePipeline](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-deleteimagepipeline)：删除镜像模板。

## `ecctl ecs image-pipeline get`

调用 API：

- [DescribeImagePipelines](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describeimagepipelines)：查询镜像模板的详细信息。
- [DescribeImagePipelineExecutions](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describeimagepipelineexecutions)：查询镜像构建任务的详细信息。

注意事项：该查询复用列表 API，通过资源标识或过滤条件收敛到单个资源。 指定 `--with-executions` 时附带查询流水线执行记录。

## `ecctl ecs image-pipeline list`

调用 API：

- [DescribeImagePipelines](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describeimagepipelines)：查询镜像模板的详细信息。

## `ecctl ecs image-pipeline start`

调用 API：

- [StartImagePipelineExecution](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-startimagepipelineexecution)：通过镜像模板执行构建镜像的任务。
- [DescribeImagePipelineExecutions](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describeimagepipelineexecutions)：查询镜像构建任务状态和结果。

注意事项：镜像构建是异步任务，默认等待执行完成并回读任务结果。

## `ecctl ecs image-pipeline cancel`

调用 API：

- [CancelImagePipelineExecution](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-cancelimagepipelineexecution)：取消镜像构建任务。
- [DescribeImagePipelineExecutions](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describeimagepipelineexecutions)：查询镜像构建任务状态。

注意事项：取消执行是异步操作，默认等待执行进入取消或终止状态。

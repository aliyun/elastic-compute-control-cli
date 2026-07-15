# ecs elasticity-assurance

资源：弹性保障

优先级：P3

别名：`eap`

本文件只描述 `ecctl ecs elasticity-assurance` 的 interface 级命令设计：每个操作命令对应哪些 ECS API，不展开 flag、参数结构和输出结构。

## `ecctl ecs elasticity-assurance create`

调用 API：

- [CreateElasticityAssurance](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-createelasticityassurance)：创建弹性保障服务。
- [PurchaseElasticityAssurance](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-purchaseelasticityassurance)：购买一个准备完毕的弹性保障服务。

注意事项：购买弹性保障也归入创建入口。

## `ecctl ecs elasticity-assurance update`

调用 API：

- [ModifyElasticityAssurance](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-modifyelasticityassurance)：修改弹性保障服务信息。
- [ModifyElasticityAssuranceAutoRenewAttribute](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-modifyelasticityassuranceautorenewattribute)：修改弹性保障服务自动续费。
- [DescribeElasticityAssurances](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describeelasticityassurances)：回读弹性保障服务状态和资源视图。
- [DescribeElasticityAssuranceAutoRenewAttribute](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describeelasticityassuranceautorenewattribute)：回读弹性保障服务自动续费。

注意事项：指定 `--auto-renew` 时调用 `ModifyElasticityAssuranceAutoRenewAttribute`。涉及弹性保障服务信息的异步变更时，默认等待目标状态并回读资源视图；自动续费变更默认回读自动续费状态。

## `ecctl ecs elasticity-assurance get`

调用 API：

- [DescribeElasticityAssurances](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describeelasticityassurances)：查询弹性保障服务的信息。
- [DescribeElasticityAssuranceAutoRenewAttribute](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describeelasticityassuranceautorenewattribute)：查询弹性保障服务自动续费。
- [DescribeElasticityAssuranceInstances](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describeelasticityassuranceinstances)：查询弹性保障服务已匹配实例列表。

注意事项：该查询复用列表 API，通过资源标识或过滤条件收敛到单个资源。 指定 `--with-auto-renew` 时附带查询自动续费属性。 指定 `--with-instances` 时附带查询弹性保障关联实例。

## `ecctl ecs elasticity-assurance list`

调用 API：

- [DescribeElasticityAssurances](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describeelasticityassurances)：查询弹性保障服务的信息。

## `ecctl ecs elasticity-assurance renew`

调用 API：

- [RenewElasticityAssurances](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-renewelasticityassurances)：续费弹性保障服务。

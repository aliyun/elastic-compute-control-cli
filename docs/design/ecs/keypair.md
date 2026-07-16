# ecs keypair

资源：密钥对

优先级：P1

本文件只描述 `ecctl ecs keypair` 的 interface 级命令设计：每个操作命令对应哪些 ECS API，不展开 flag、参数结构和输出结构。

## `ecctl ecs keypair create`

调用 API：

- [CreateKeyPair](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-createkeypair)：创建密钥对。
- [ImportKeyPair](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-importkeypair)：导入密钥对公钥。

注意事项：导入已有公钥也归入创建入口。

## `ecctl ecs keypair delete`

调用 API：

- [DeleteKeyPairs](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-deletekeypairs)：批量删除密钥对。

## `ecctl ecs keypair get`

调用 API：

- [DescribeKeyPairs](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describekeypairs)：查询密钥对列表。

注意事项：该查询复用列表 API，通过资源标识或过滤条件收敛到单个资源。

## `ecctl ecs keypair list`

调用 API：

- [DescribeKeyPairs](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-describekeypairs)：查询密钥对列表。

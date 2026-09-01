# ecs keypair

资源：密钥对

优先级：P1

本文件只描述 `ecctl ecs keypair` 的 interface 级命令设计：每个操作命令对应哪些 ECS API，不展开 flag、参数结构和输出结构。

## `ecctl ecs keypair create`

调用 API：

- [CreateKeyPair](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-createkeypair)：创建密钥对。
- [ImportKeyPair](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-importkeypair)：导入密钥对公钥。

注意事项：导入已有公钥也归入创建入口。`CreateKeyPair` 返回的私钥只在创建响应中出现，
ecctl 将其原样放在成功输出的 `keypair.private_key` 字段中，不自动写本地文件；用户必须
立即将该字段安全保存。自动生成路径直接使用 `CreateKeyPair` 响应构造结果，不执行可能
导致一次性私钥丢失的后续回读；`ImportKeyPair` 不生成私钥，创建后通过
`DescribeKeyPairs` 回读资源视图，后续 `get` 和 `list` 也不会返回私钥。

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

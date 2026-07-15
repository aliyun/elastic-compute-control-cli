# lingjun net-test

资源：灵骏网络压测任务

优先级：P2

本文件只描述 `ecctl lingjun net-test` 的 interface 级命令设计：每个操作命令对应哪些 eflo-controller API，不展开完整 flag、参数结构和输出结构；仅在多 API 分流时标明必要的特殊开关。

设计目标：覆盖灵骏 GPU 集群专属的网络压测任务（NetTest）。NetTest 用于检测集群内节点间 GPU 网络的带宽、延迟和连通性。任务模型与 ECS diagnostic 类似：`create` 触发新任务，`list`/`get` 查询结果。任务由 `net-test-id` 标识，任务一旦提交不可修改/取消，结果保留，因此不提供 `update` 和 `delete` 子命令。

特殊开关命名是 interface 级建议，用来说明何时触发额外 API；最终字段名和参数细节在资源 spec 中定义。

## `ecctl lingjun net-test create`

调用 API：

- [CreateNetTestTask](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-controller-2022-12-15-createnettesttask)：创建网络压测任务。
- [DescribeNetTestResult](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-controller-2022-12-15-describenettestresult)：轮询并回读压测结果详情。

注意事项：`CreateNetTestTask` 是异步任务，默认等待结果生成完成并回读结果详情；高级用户可 `--no-wait` 立即返回任务标识。压测类型与待测节点集合由 `--type` 和 `--nodes` 指定。
形态：`ecctl lingjun net-test create --cluster <c-xxx> --type <type> --nodes <node-id>,<node-id> ...`

## `ecctl lingjun net-test get`

调用 API：

- [DescribeNetTestResult](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-controller-2022-12-15-describenettestresult)：查询单个网络压测任务结果详情。

注意事项：通过 `net-test-id` 定位单个压测结果，返回压测任务的完整执行详情与指标数据。
形态：`ecctl lingjun net-test get <net-test-id>`

## `ecctl lingjun net-test list`

调用 API：

- [ListNetTestResults](https://help.aliyun.com/zh/pai/developer-reference/api-eflo-controller-2022-12-15-listnettestresults)：查询网络压测结果列表。

注意事项：ListNetTestResults 元数据未提供 ClusterId 过滤参数，因此 `list` 不支持 `--cluster` 过滤；如需按集群筛选，请客户端处理或回到 `ecctl lingjun net-test get` 单点查询。

分页约束：ListNetTestResults 使用 NextToken/MaxResults 分页（如官方实际为 PageNumber/PageSize 请按官方描述），默认 `--limit 100`，最大值以官方 API 文档为准。
形态：`ecctl lingjun net-test list [--filter ...] [--limit 100]`

# ack kubeconfig

资源：KubeConfig

优先级：P0

别名：`kc`

本文件只描述 `ecctl ack kubeconfig` 的 interface 级命令设计：每个操作命令对应哪些 ACK API，不展开完整 flag、参数结构和输出结构；仅在多 API 分流时标明必要的特殊开关。

设计目标：覆盖 KubeConfig 的签发、状态查询、过期时间更新和吊销。`kubeconfig` 是 Agent 进入 Kubernetes 资源面的跳板：拿到 kubeconfig 后由 `kubectl` 操作 Pod、Deployment、Service 等原生资源。具体约束如下：

- `create` 统一承载签发动作：不带 `--user-id` 为当前调用方签发（自助），带 `--user-id` 为指定子账号或 RAM 角色代签发（主账号场景）。不再独立设计 `kubeconfig issue` 子命令——签发就是创建凭证，应该用 `create` 而非自定义动词，与 cli-design-rules"action 词表固定"规则对齐。
- `create` 输出 kubeconfig 原文到 stdout，可直接重定向到 `~/.kube/config` 或写入 `KUBECONFIG` 指向的文件，无需额外解析。
- `ecctl ack` 不集成 `kubectl`，也不在 ACK 命令面下提供 K8s 资源面操作；获取 kubeconfig 之后的所有 K8s 行为显式交给 `kubectl`，避免命令面与 kubectl 责任重叠。
- `revoke` 是吊销已颁发的 kubeconfig 凭证（使其失效），不是删除资源对象，因此独立成领域动作，不归入 `delete`。

特殊开关命名是 interface 级建议，用来说明何时触发额外 API；最终字段名和参数细节在资源 spec 中定义。

## `ecctl ack kubeconfig create`

调用 API：

- 默认调用 [DescribeClusterUserKubeconfig](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-describeclusteruserkubeconfig)：为当前调用方（AccessKey 对应的用户）签发并返回 kubeconfig 原文。
- 指定 `--user-id` 时调用 [DescribeSubaccountK8sClusterUserConfig](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-describesubaccountk8sclusteruserconfig)：主账号为指定子账号或 RAM 角色代签发 kubeconfig。

注意事项：`create` 表达"创建 kubeconfig 凭证"，自助签发与代签发共用同一动词，仅通过 `--user-id` 是否指定分流到两个底层 API，不独立设计 `kubeconfig issue`。两种模式 stdout 输出格式一致，可直接重定向给 `kubectl`，CLI 不做额外解析或落盘。重复 `create` 会覆盖签发新的凭证；如需先吊销旧凭证再签发，先调用 `revoke` 再 `create`。

## `ecctl ack kubeconfig update`

调用 API：

- [UpdateK8sClusterUserConfigExpire](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-updatek8sclusteruserconfigexpire)：更新 kubeconfig 自定义过期时间。

注意事项：`update` 仅承载 kubeconfig 过期时间的调整。kubeconfig 没有名称、描述等其他可变属性，因此不做多 API 分流。

## `ecctl ack kubeconfig get`

调用 API：

- [DescribeClusterUserKubeconfig](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-describeclusteruserkubeconfig)：查询当前调用方在指定集群已签发的 kubeconfig 内容。

注意事项：`get` 用于读取当前调用方在指定集群已颁发的 kubeconfig；与 `create` 的区别是 `get` 不重新签发、仅读取已存在凭证（底层 API 同名，但 CLI 语义按读/写区分）。要查看签发状态（颁发数量、过期时间、是否吊销）走 `list`。

## `ecctl ack kubeconfig list`

调用 API：

- 默认调用 [ListClusterKubeconfigStates](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-listclusterkubeconfigstates)：查询指定集群已颁发的 kubeconfig 列表及状态。
- 指定 `--scope user` 时调用 [ListUserKubeConfigStates](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-listuserkubeconfigstates)：查询当前调用方在所有集群的 kubeconfig 状态。

注意事项：默认按集群维度列出 kubeconfig 状态；`--scope user` 切到用户维度，跨集群查询当前调用方的 kubeconfig 颁发情况，便于排查"哪些集群已经签发过 kubeconfig"。

## `ecctl ack kubeconfig revoke`

调用 API：

- [RevokeK8sClusterKubeConfig](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-revokek8sclusterkubeconfig)：吊销当前用户在指定集群的 kubeconfig。

注意事项：`revoke` 是凭证吊销动作（使已颁发的 kubeconfig 失效），不删除任何 ACK 资源对象，因此独立成领域动作而不归入 `delete`。吊销后再次执行 `create` 会重新签发新的 kubeconfig。

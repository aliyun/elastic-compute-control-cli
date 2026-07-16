# ack permission

资源：权限

优先级：P1

本文件只描述 `ecctl ack permission` 的 interface 级命令设计：每个操作命令对应哪些 ACK API，不展开完整 flag、参数结构和输出结构；仅在多 API 分流时标明必要的特殊开关。

设计目标：覆盖 RAM 用户/角色对 ACK 集群的授权与回收。授权对象包括 ACK 内置角色（cluster-admin / dev / ops / restricted 等）和自定义 RBAC 角色。围绕授权语义有四个关键约束：

1. `update` 同时承载覆盖式与增量授权：默认调用 `UpdateUserPermissions` 做增量调整（追加 / 移除指定 RBAC 项），适合在不影响其他授权的前提下做局部变更；指定 `--replace` 时切换到 `GrantPermissions`，整体替换该用户的集群授权关系，未列入的集群授权会被收回。`grant` / `update` 不再独立成两个动作。
2. `delete` 区分单集群和全集群：默认调用 `CleanClusterUserPermissions` 限定到单个集群，撤销该用户在该集群的 KubeConfig 与 RBAC；指定 `--all-clusters` 时切到 `CleanUserPermissions`，一键清理用户在所有集群的 KubeConfig 与 RBAC，适用于离职 / 吊销场景。
3. RBAC 视图本质是 K8s 资源面（RoleBinding / ClusterRoleBinding），但授权动作走 ACK 控制面 API：ACK 把授权语义抽象到 OpenAPI 上，由控制面写入对应集群的 RBAC 对象。`ecctl ack permission` 只调用 ACK 控制面 API，不直接操作 K8s 资源面；用户若要直接看 RBAC 对象，仍需通过 `kubectl get rolebinding/clusterrolebinding`。
4. 服务关联角色检查（`CheckServiceRole`）不属于用户授权语义，已移入 [README.md](README.md) 的"暂不进入主命令面的 API"，不在本资源下设计独立动作。

特殊开关命名是 interface 级建议，用来说明何时触发额外 API；最终字段名和参数细节在资源 spec 中定义。

## `ecctl ack permission update`

调用 API：

- 默认调用 [UpdateUserPermissions](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-updateuserpermissions)：增量更新 RBAC 权限（追加 / 移除指定项）。
- 指定 `--replace` 时调用 [GrantPermissions](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-grantpermissions)：覆盖式授权，整体替换该用户的集群授权关系。
- [DescribeUserPermission](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-describeuserpermission)：回读变更后的授权视图。

注意事项：默认增量语义只对请求中显式列出的授权项做追加 / 移除，不影响该用户在其他集群的现有授权；指定 `--replace` 切换到覆盖式语义，请求体表达的是该用户的目标授权全集，未列入的集群授权会被收回，适合声明式同步、初始化或重置场景。两个 API 不再独立设计 `permission grant`，统一收敛在 `update` 下。授权写入存在最终一致延迟，默认等待变更可见并回读。

## `ecctl ack permission delete`

调用 API：

- 默认调用 [CleanClusterUserPermissions](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-cleanclusteruserpermissions)：清理指定用户在指定集群的 KubeConfig 与 RBAC。
- 指定 `--all-clusters` 时调用 [CleanUserPermissions](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-cleanuserpermissions)：清理用户在所有集群的 KubeConfig 与 RBAC。
- [DescribeUserPermission](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-describeuserpermission)：回读清理后的授权视图，确认 RBAC 与 KubeConfig 已撤销。

注意事项：`delete` 同时撤销 RBAC 授权和已签发的 KubeConfig，是终止用户对集群访问的统一入口。默认作用于单个集群，需要 `--cluster-id`；`--all-clusters` 切换为全集群清理，用于离职、密钥泄露、整体吊销等场景，调用前应明确不可逆。两条路径不再独立设计 `permission clean`，统一收敛在 `delete` 下，并保留 `--force=false` 默认值，避免误删。两条路径都是异步操作，默认等待清理完成并回读。

## `ecctl ack permission get`

调用 API：

- [DescribeUserPermission](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-describeuserpermission)：查询某 RAM 用户/角色被授权的集群与角色详情。

注意事项：`--user-id` 必填，标识被查询的 RAM 用户或角色。该 API 同时承担 `get` 和 `list` 视图；`get` 聚焦单用户的完整授权详情，`list` 用客户端过滤兜底集群维度的反向视图。

## `ecctl ack permission list`

调用 API：

- [DescribeUserPermission](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-describeuserpermission)：查询某 RAM 用户/角色被授权的集群列表与角色。

注意事项：`--user-id` 必填。`list` 视图聚焦"该用户在哪些集群被授予了什么角色"，集群维度的反向视图（"该集群有哪些用户被授权"）当前 OpenAPI 不直接支持，由 `ack permission list` 在客户端按集群过滤兜底。

## 暂不进入主命令面的 API

- [CheckServiceRole](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-checkservicerole)：检查 ACK 服务关联角色（service-linked role）授权状态，不属于用户授权语义；引导式调用即可，需要时走 `ecctl aliyun cs CheckServiceRole`。

K8s 资源面的 RBAC 直接对象（RoleBinding / ClusterRoleBinding / Role / ClusterRole）由 `kubectl` 操作，不在 `ecctl ack permission` 范围内。

## 废弃/不推荐 API

无。

# ack audit

资源：审计

优先级：P2

本文件只描述 `ecctl ack audit` 的 interface 级命令设计：每个操作命令对应哪些 ACK API，不展开完整 flag、参数结构和输出结构；仅在多 API 分流时标明必要的特殊开关。

设计目标：覆盖 ACK 集群审计与控制面日志配置两类对象。`audit` 资源下并列两个对象：集群 API Server 审计日志（cluster audit log），通过 `get` / `update` 直接操作；控制面组件日志（kube-apiserver / kube-controller-manager / kube-scheduler 等），通过 `control-plane-log get` / `control-plane-log update` 子资源动作操作。两者都是集群级单实例配置，没有 list；配置开关由 `update` 内部 flag 表达，不设独立的 create/delete/enable/disable 子命令。

特殊开关命名是 interface 级建议，用来说明何时触发额外 API；最终字段名和参数细节在资源 spec 中定义。

## `ecctl ack audit update`

调用 API：

- [UpdateClusterAuditLogConfig](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-updateclusterauditlogconfig)：开启、关闭或更新集群审计日志配置（绑定 SLS Project、保留期等）。

注意事项：开关与配置变更统一由 `update` 承载，开/关由 update 内部 flag（如 `--enabled`）表达，不单独设计 `enable` / `disable` 子命令。变更是异步生效，默认等待配置生效后回读 `GetClusterAuditProject`。

## `ecctl ack audit get`

调用 API：

- [GetClusterAuditProject](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-getclusterauditproject)：查询集群审计日志的 SLS Project / Logstore 配置。

注意事项：审计日志是集群级单实例配置，没有 list；返回是否开启、关联的 SLS Project、Logstore 与保留期等。

## `ecctl ack audit control-plane-log update`

调用 API：

- [UpdateControlPlaneLog](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-updatecontrolplanelog)：修改 ACK 托管集群控制面组件日志配置（开关、组件选择、SLS Project）。

注意事项：组件粒度的开/关与 SLS Project 绑定都由 `update` 承载，开/关由 update 内部 flag（如 `--components`、`--enabled`）表达，不单独设计 `enable` / `disable` 子命令。变更是异步生效，默认等待配置生效后回读 `CheckControlPlaneLogEnable`。仅适用于 ACK 托管集群（专有版控制面组件运行在用户节点上，不通过此 API 配置）。

## `ecctl ack audit control-plane-log get`

调用 API：

- [CheckControlPlaneLogEnable](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-checkcontrolplanelogenable)：查询托管集群控制面组件日志配置（哪些组件已开启、关联 SLS Project）。

注意事项：控制面组件日志是集群级单实例配置，没有 list；返回各组件（kube-apiserver / kube-controller-manager / kube-scheduler 等）的开启状态。

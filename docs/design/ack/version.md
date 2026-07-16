# ack version

资源：Kubernetes 版本元数据

优先级：P2

别名：无

本文件只描述 `ecctl ack version` 的 interface 级命令设计：每个操作命令对应哪些 ACK API，不展开 flag、参数结构和输出结构。

## `ecctl ack version list`

调用 API：

- [DescribeKubernetesVersionMetadata](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-describekubernetesversionmetadata)：查询某地域、某集群类型下支持的 Kubernetes 版本列表与元数据（含支持的 runtime、组件等）。

注意事项：按 `region + cluster_type` 分区查询，全量一次返回（无分页），返回该范围内的全部可用版本与升级路径，供 `cluster create/upgrade` 在客户端做版本与组件兼容性校验。

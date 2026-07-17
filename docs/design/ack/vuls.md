# ack vuls

资源：漏洞

优先级：P1

本文件只描述 `ecctl ack vuls` 的 interface 级命令设计：每个操作命令对应哪些 ACK API，不展开完整 flag、参数结构和输出结构；仅在多 API 分流时标明必要的特殊开关。

设计目标：覆盖集群和节点池粒度的漏洞扫描和详情查询。要点：

1. `create` 表达"创建一次新扫描任务"，与 `ack check create` / `ack inspect report create` / `ack diagnosis create` 等异步任务创建语义对齐；不再使用 `vuls scan`。
2. `list` 在集群和节点池两种粒度间根据 flag 分流：默认查询集群漏洞，指定节点池时切换到节点池粒度。
3. 漏洞修复（fix）属于节点池修复维度，由 `ack nodepool repair --vulnerabilities` 承载，本资源不重复定义。
4. ACK 不提供修改和删除漏洞扫描记录的 API，因此没有 `update` / `delete`。

特殊开关命名是 interface 级建议，用来说明何时触发额外 API；最终字段名和参数细节在资源 spec 中定义。

## `ecctl ack vuls create`

调用 API：

- [ScanClusterVuls](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-scanclustervuls)：触发集群漏洞扫描。
- [DescribeClusterVuls](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-describeclustervuls)：回读扫描后的漏洞详情。

注意事项：`vuls create` 表达"创建一次新漏洞扫描任务"。`ScanClusterVuls` 是异步扫描接口，默认等待扫描进入终态并回读集群漏洞详情；指定 `--no-wait` 时只返回 `task_id`，由 `ack task` 命令面跟进或调用方再次 `vuls list` 拉取。

## `ecctl ack vuls list`

调用 API：

- 默认调用 [DescribeClusterVuls](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-describeclustervuls)：列出集群漏洞详情。
- 指定 `--nodepool <id>` 时调用 [DescribeNodePoolVuls](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-describenodepoolvuls)：列出节点池粒度漏洞详情。

注意事项：`list` 在集群和节点池两种粒度间根据 `--nodepool` 分流，不单独设计 `ack vuls nodepool list` 子命令。漏洞按 CVE 维度聚合输出，不再为不同严重程度单独设计命令；过滤通过 `list --severity` 等 flag 表达。

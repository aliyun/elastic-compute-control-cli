# ack auto-repair-policy

资源：节点自愈规则

优先级：P2

别名：`arp`

本文件只描述 `ecctl ack auto-repair-policy` 的 interface 级命令设计：每个操作命令对应哪些 ACK API，不展开完整 flag、参数结构和输出结构；仅在多 API 分流时标明必要的特殊开关。

设计目标：覆盖节点自愈规则的标准 CRUD 全生命周期。要点：

1. 标准 `create / update / delete / get / list` 五个动作即可承载全部 API，没有领域动作。
2. 与 `ack nodepool repair` 的语义边界清晰：`auto-repair-policy` 是声明式的策略对象，描述"在节点不健康时应当如何自动触发修复"；`nodepool repair` 是命令式的领域动作，由用户手动触发对节点池中异常节点的一次性修复。前者是规则，后者是动作，不互相替代。
3. 自愈规则对象本身没有异步状态机，CRUD 调用同步生效，不需要 waiter；策略匹配后实际触发的修复任务归属于节点池侧。

特殊开关命名是 interface 级建议，用来说明何时触发额外 API；最终字段名和参数细节在资源 spec 中定义。

## `ecctl ack auto-repair-policy create`

调用 API：

- [CreateAutoRepairPolicy](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-createautorepairpolicy)：创建节点自愈规则。
- [DescribeAutoRepairPolicy](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-describeautorepairpolicy)：回读创建后的规则视图。

注意事项：创建是同步操作，默认回读规则视图确认配置生效。

## `ecctl ack auto-repair-policy update`

调用 API：

- [ModifyAutoRepairPolicy](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-modifyautorepairpolicy)：修改自愈规则。
- [DescribeAutoRepairPolicy](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-describeautorepairpolicy)：回读修改后的规则视图。

注意事项：修改是同步操作，默认回读规则视图。

## `ecctl ack auto-repair-policy delete`

调用 API：

- [DeleteAutoRepairPolicy](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-deleteautorepairpolicy)：删除自愈规则。

## `ecctl ack auto-repair-policy get`

调用 API：

- [DescribeAutoRepairPolicy](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-describeautorepairpolicy)：查询单条自愈规则详情。

## `ecctl ack auto-repair-policy list`

调用 API：

- [ListAutoRepairPolicies](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-listautorepairpolicies)：列出全部自愈规则。

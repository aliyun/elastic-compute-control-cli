# ack template

资源：编排模板

优先级：P2

本文件只描述 `ecctl ack template` 的 interface 级命令设计：每个操作命令对应哪些 ACK API，不展开完整 flag、参数结构和输出结构；仅在多 API 分流时标明必要的特殊开关。

设计目标：覆盖编排模板的标准 CRUD（创建、更新、删除、单查、列表），便于在多个集群中复用同一份应用编排。模板内容是 YAML 字符串，长内容通过 `--content @file.yaml` 走文件读取，避免在命令行直接拼接多行 YAML。模板属于阿里云控制面元数据，仅承载文本本身；将模板真正部署到集群是 K8s 资源面动作，由 `kubectl apply -f` 或其他动作命令配合 `ack kubeconfig get` 后完成，不在 `template` 资源命令面内表达。

特殊开关命名是 interface 级建议，用来说明何时触发额外 API；最终字段名和参数细节在资源 spec 中定义。

## `ecctl ack template create`

调用 API：

- [CreateTemplate](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-createtemplate)：创建编排模板。
- [DescribeTemplateAttribute](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-describetemplateattribute)：回读模板详情。

注意事项：模板 YAML 内容通过 `--content` 传入，长内容支持 `--content @file.yaml` 从文件读取。`CreateTemplate` 是同步接口，创建后回读一次模板详情以输出资源视图。模板创建只生成模板元数据，不会触发任何集群部署；如需部署，使用 `kubectl apply -f` 配合 `ack kubeconfig get`。

## `ecctl ack template update`

调用 API：

- [UpdateTemplate](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-updatetemplate)：更新模板。
- [DescribeTemplateAttribute](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-describetemplateattribute)：回读模板更新后的详情。

注意事项：模板名、描述、标签、模板内容等属性变更全部归入 `update`，不再独立设计 `rename`、`content set` 等子命令。模板内容通过 `--content` 传入，长内容支持 `--content @file.yaml` 从文件读取。`UpdateTemplate` 只更新模板文本本身，不会同步刷新已基于该模板部署到集群中的工作负载；存量工作负载的对齐由用户使用 `kubectl apply -f` 配合 `ack kubeconfig get` 重新下发。

## `ecctl ack template delete`

调用 API：

- [DeleteTemplate](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-deletetemplate)：删除模板。

注意事项：删除只移除模板元数据本身，不会回收基于该模板已部署到集群的工作负载；如需清理集群侧资源，使用 `kubectl delete` 配合 `ack kubeconfig get`。

## `ecctl ack template get`

调用 API：

- [DescribeTemplateAttribute](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-describetemplateattribute)：查询单个模板详情。

注意事项：默认输出包含模板元数据与 YAML 内容；模板内容仅作文本返回，部署需要由 `kubectl apply -f` 配合 `ack kubeconfig get` 触发。

## `ecctl ack template list`

调用 API：

- [DescribeTemplates](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-describetemplates)：列出全部模板。

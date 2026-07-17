# ack diagnosis

资源：诊断

优先级：P1

别名：`diag`

本文件只描述 `ecctl ack diagnosis` 的 interface 级命令设计：每个操作命令对应哪些 ACK API，不展开完整 flag、参数结构和输出结构；仅在多 API 分流时标明必要的特殊开关。

设计目标：覆盖集群专项问题排查场景（节点、网络、负载、Ingress 等），一次诊断 = 一份诊断报告。需要注意：

1. 诊断是**一次性触发 + 异步**操作：`create` 触发后默认等待诊断完成并回读结果，避免用户再手工拉取一次 `get`。
2. `check-item` 是 `diagnosis` 的子对象，承载诊断类型的检查项目录（元信息），只有 `list` 一个动作；与具体诊断实例无关。
3. **不提供 `list`**：底层 API 不支持诊断历史列举；用户需自行记录 `create` 返回的诊断 ID，后续通过 `get <diagnosisId>` 复读。
4. **不提供 `delete`**：底层 API 不支持，诊断报告由平台侧生命周期管理。

特殊开关命名是 interface 级建议，用来说明何时触发额外 API；最终字段名和参数细节在资源 spec 中定义。

## `ecctl ack diagnosis create`

调用 API：

- [CreateClusterDiagnosis](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-createclusterdiagnosis)：触发一次集群诊断。
- [GetClusterDiagnosisResult](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-getclusterdiagnosisresult)：等待并回读诊断结果。

注意事项：通过 `--type node|network|ingress|...` 指定诊断类型，并提供该类型所需的目标对象（例如节点名、Service 名、Ingress 名等）。`CreateClusterDiagnosis` 是异步接口，返回诊断 ID；默认等待诊断进入终态并调用 `GetClusterDiagnosisResult` 回读结果，无需用户再手工 `get`。指定 `--no-wait` 时只返回诊断 ID，由 `ack task` 命令面跟进或调用方再次 `diagnosis get` 拉取。

## `ecctl ack diagnosis get`

调用 API：

- [GetClusterDiagnosisResult](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-getclusterdiagnosisresult)：查询指定诊断 ID 的诊断报告。

注意事项：诊断 ID 来自 `create` 的返回值，用户需自行保存。该 API 同时承担 `create` 的等待回读和事后复读两种用途。

## `ecctl ack diagnosis check-item list`

调用 API：

- [GetClusterDiagnosisCheckItems](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-getclusterdiagnosischeckitems)：列出指定诊断类型的检查项目录。

注意事项：`check-item` 是 `diagnosis` 的子对象，承载"某诊断类型会检查哪些维度"的元信息；只暴露 `list` 一个词表内动作，参照 ecs `diagnostic metric-set list` 子对象模式，不再使用 `diagnosis check-items` 这种非词表动词。通过 `--type` 指定诊断类型；不需要诊断 ID，也不返回诊断结果。

## 暂不进入主命令面的 API

无。诊断领域全部 API 已纳入主命令面；不设计 `list` / `delete` 的原因见开头设计目标的第 3、4 点。

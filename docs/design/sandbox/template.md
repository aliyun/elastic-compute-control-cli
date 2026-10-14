# sandbox template

资源：sandbox template（默认 E2B；`--openapi` 选择原生 FC OpenAPI）。

优先级：P1

命令路径为 `ecctl sandbox template`，也支持产品别名 `sbx` 和资源别名
`tpl`。该资源与 `sandbox/sandbox` 使用相同的全局端点、
`E2B_API_KEY` 和安全策略。

同一产品下的 `ecctl sandbox team`（`sbx team`）使用独立的 FCSandbox
OpenAPI AK/STS 凭证及目标地域；模板的原生 FC 模式使用同样的凭证与地域规则，
未指定 `--openapi` 时继续使用上述 E2B 路由和凭证。
详见 [Team 路由说明](team.md)。

## 原生 FC OpenAPI

五个共有操作可显式指定 `--openapi`。该选择不取决于 E2B 密钥、URL、
DNS、profile 或地域，也不改变 `ECCTL_SANDBOX_BACKEND=fc` 的既有
E2B 兼容含义。没有 HTTP 错误后的自动跨 API 回退。

| 操作 | FCSandbox 2026-05-09 ROA | 团队作用域 |
|---|---|---|
| `create` | `POST /pop/2026-05-09/templates`，CreateTemplate | `body.teamID` |
| `get` | `GET /pop/2026-05-09/templates/{templateID}`，GetTemplate | `query.teamID` |
| `list` | `GET /pop/2026-05-09/templates`，ListTemplates | `query.teamID` |
| `update` | `PUT /pop/2026-05-09/templates/{templateID}`，UpdateTemplate | `query.teamID` |
| `delete` | `DELETE /pop/2026-05-09/templates/{templateID}`，DeleteTemplate | `query.teamID` |

原生模式必须从 flag、环境或 profile 解析目标地域，使用对应的
`fcsandbox.<region>.aliyuncs.com` 端点和 Aliyun profile AK/STS。
E2B 的 Key、URL、domain、后端和 CA 配置不影响它，也不能替代 Aliyun 凭证。
`sandbox` 默认资源和未指定 `--openapi` 的模板保持全局 E2B 语义。

```bash
ecctl sbx template create --openapi --region cn-hangzhou --team-id <team-id> \
  --name python --runtime-config @runtime.json --build-config @build.json
ecctl sbx template get <template-id> --openapi --region cn-hangzhou --team-id <team-id>
ecctl sbx template list --openapi --region cn-hangzhou --team-id <team-id> --limit 20
ecctl sbx template update <template-id> --openapi --region cn-hangzhou \
  --team-id <team-id> --runtime-config '{"internetAccess":false}'
ecctl sbx template delete <template-id> --openapi --region cn-hangzhou --team-id <team-id>
```

`runtimeConfig` 支持数值 CPU、整数 MB 内存和磁盘、公网开关、VPC、日志及
sandbox 镜像配置；`buildConfig` 支持 `copy` 和 `envdInject`。嵌套配置保持
JSON 的 camelCase 键、数值、布尔和数组类型，支持 JSON 或 `@file`。
启用 `envdInject.enabled` 时，还需启用 `copy.enabled`，例如
`--build-config '{"copy":{"enabled":true},"envdInject":{"enabled":true}}'`。
Team 的列表可能包含服务内置模板；验证创建结果应按模板 ID 和 `team_id`
匹配，不能假定列表只有一条结果或固定顺序。
仓库密码和用户名等认证配置应通过私有 `@file` 输入，不进入 shell history。
当前内嵌的 `aliyun/aliyun-openapi-meta` 快照（revision
`3c5b9bdc7d9f06fe06b5793713845820121f1f17`）不含 `installStorageClients`。
这是内嵌快照的验证范围，不代表官方 API 版本未提供该字段；未覆盖的字段仍
明确拒绝，需随内嵌快照更新后再开放。

更新严格使用 API 实际引用的 `PublicUpdateTemplateInput`，仅接受
`runtimeConfig` / `buildConfig`，至少提供一项；不发送 `name`、`public`
或仅创建支持的 `sandboxConfig.steps`。显式混用两个 API 的参数会在读取
凭证或发请求前报错。E2B 专用构建、标签、公开/私有操作没有原生路由。

创建和更新默认通过保持团队作用域的 GetTemplate 回读，2 秒间隔、默认
15 分钟超时，`--timeout` 可调整。只有 `status.state=ready` 才报告就绪；
`error` / `failed` 返回非零，附带脱敏后的 reason 和 GetTemplate 请求记录。
未知状态继续等待至超时。`--no-wait` 仅返回模板 ID 和提供的团队上下文。
原生创建和更新使用 `--no-wait` 且未显式指定 `--timeout` 时，请求预算为
30 秒。等待模式的 `--timeout` 必须大于零，在读取凭证或发请求前拒绝非正值；
`--no-wait --timeout 0s` 保留不设置操作期限的行为。其他显式 `--timeout`
保留其原值，上游 context 的更早期限和取消仍生效。
凭证必须覆盖实际请求期限，普通构建等待的 15 分钟预算及凭证寿命校验保持不变。
若创建成功而随后等待失败，错误携带固定地域、团队和 profile 的
`recovery_command`，供用户或验证后的 E2E 清理 journal 删除该次新建模板。

Get/List 输出 `name`、`team_id`、`team_name`、`resource_group`、
`runtime_config`、`created_time`、`build_status`、`reason`、`finished_at`，
后面三项来自 `status.state/reason/finishedAt`。List 使用服务端
`maxResults` / `nextToken`，保留 CLI 的 `--limit` / `--next-token`；不使用
E2B 兼容 FC 的本地游标。原生 Get 不接受 E2B builds 的分页参数。

在 provider 边界验证小写业务 `code/message/requestId`，HTTP 200 的非
`"200"` 业务码仍失败。缺失或错误 ID、团队不符、无效状态、非数组列表、
重复 ID 或无效分页字段不能变成成功空结果。配置中的认证字段、请求 ID、
业务错误和构建原因中的已知密码会在进入输出及遥测前脱敏。
自由文本中出现敏感赋值时，整个文本值会脱敏，避免保留边界不明确的密码后缀；
API 代码、请求 ID 等结构化证据仍可用于定位，已验证的分页 token 原样保留。

依据：[官方元数据](https://api.aliyun.com/meta/v1/products/FCSandbox/versions/2026-05-09/api-docs.json)，
[CreateTemplate](https://api.aliyun.com/document/FCSandbox/2026-05-09/CreateTemplate)、
[GetTemplate](https://api.aliyun.com/document/FCSandbox/2026-05-09/GetTemplate)、
[ListTemplates](https://api.aliyun.com/document/FCSandbox/2026-05-09/ListTemplates)、
[UpdateTemplate](https://api.aliyun.com/document/FCSandbox/2026-05-09/UpdateTemplate)、
[DeleteTemplate](https://api.aliyun.com/document/FCSandbox/2026-05-09/DeleteTemplate)。

原生 E2E 位于 `e2e/cases/sandbox/team-lifecycle.yaml`，创建
run-specific Team 和 Template，清理顺序为 Template 后 Team；需要目标地域
私有 `sandbox.template.runtime_config_file` 镜像 fixture。现有 E2B lifecycle
继续覆盖默认模板协议。registry 的原生五项只有在对应案例 live 通过后才能
标记 `live-pass`。

原生 E2E 在创建前固定 profile、配置文件的绝对请求路径和解析目标，
并保存可信 STS 返回的公开账户、用户或角色身份。已拥有模板的
get/list/update/delete 沿用该绑定，路径或主体变化会在资源 API 请求前失败。
journal 登记创建意图的确切路径；重放统一解析 journal 的真实目标，
核对每条原生删除对应的归属、配置和身份记录。缺失意图、复制或换名导致
归属路径不符、删除目标不符时保留清理项并拒绝请求，不保存 AK 或 token。
同一 journal 中已分配的原生模板须保持同一 STS 主体；冲突会在下一次创建前拒绝。
进程未启动，或返回原生 provider 在 API 请求前产生的
`InvalidFCTemplateConfig`（client）时，保存无分配证据并允许父资源清理。
响应丢失、其他错误或空列表仍保留 pending，不能作为未分配的证明。

## 创建契约

E2B 当前模板创建是两阶段协议：

1. `POST /v3/templates` 申请 `templateID` 和 `buildID`；
2. `POST /v2/templates/{templateID}/builds/{buildID}` 提交构建计划；
3. `GET /templates/{templateID}/builds/{buildID}/status` 等待
   `ready`，`error` 为失败终态。

`ecctl sandbox template create` 按顺序编排这三个阶段，至少要求
`--from-image` 或 `--from-template` 之一，两者互斥。`--step` 可重复，
每项为 E2B `TemplateStep` JSON 或 `@file`；同时支持 start/ready command、
CPU、内存、标签、强制重建和私有镜像仓库凭证。私有仓库凭证应使用
`@file` 输入，避免明文进入 shell history 或进程参数。若 template 已申请、
但启动 build 失败，命令返回非零，并附带删除已申请 template 的恢复命令；
E2E runner 只会登记通过 cleanup delete 白名单校验的恢复命令。

官方 E2B CLI 会在本地解析 Dockerfile、打包 COPY/ADD 上下文并上传构建
层。ecctl 保持资源操作的结构化输入契约，直接接收 E2B build plan；本次
不内置第二套 Dockerfile 解释器，也不隐式读取或上传工作目录。没有
COPY/ADD 的 Dockerfile 可等价表达为 `--from-image` 加若干 `--step`。

## 资源操作

| ecctl 操作 | E2B API | 说明 |
| --- | --- | --- |
| `create` | `POST /v3/templates`、`POST /v2/templates/.../builds/...` | 创建并启动构建，默认等待 `ready`。 |
| `get` | `GET /templates/{templateID}` | 获取 template 和 builds。 |
| `list` | 原生 E2B：`GET /v2/templates`；FC：`GET /templates` | 保留 `--limit`、`--next-token` 分页接口。 |
| `update` | `PATCH /v2/templates/{templateID}` | 显式更新 public 可见性。 |
| `publish` / `unpublish` | 同上 | 对齐 E2B CLI 的公开/私有语义。 |
| `build-status` | `GET .../status` | 获取指定 build 的状态和失败原因。 |
| `build-logs` | `GET .../logs` | 获取指定 build 的结构化日志。 |
| `tag-list` | `GET /templates/{templateID}/tags` | 列出 build tags。 |
| `tag-assign` | `POST /templates/tags` | 为目标 template build 分配 tags。 |
| `tag-delete` | `DELETE /templates/tags` | 删除 tags。 |
| `delete` | `DELETE /templates/{templateID}` | 删除 template。 |

## `ecctl sandbox template create`

创建 template 并启动 build；默认等待 build 进入 `ready`。

## `ecctl sandbox template update`

显式更新 template 的 public 可见性。

## `ecctl sandbox template delete`

删除远端 template 资源。

## `ecctl sandbox template get`

获取 template 详情及其 build 列表。

## `ecctl sandbox template list`

分页列出当前项目可访问的 templates。

### 阿里云 FC 兼容

FC 的 E2B 兼容接口使用 `GET /templates` 返回完整模板数组，原生 E2B
使用 `GET /v2/templates` 和服务端分页 token。相关协议见
[FC Go SDK](https://github.com/aliyun-fc/e2b-go-sdk/blob/master/template.go) 和
[E2B OpenAPI](https://github.com/e2b-dev/E2B/blob/main/spec/openapi.yml)。

端点沿用现有配置：优先使用 `E2B_API_URL`；未配置时使用
`https://api.${E2B_DOMAIN}`；两者均未设置时，按 [Sandbox 地域解析规则](sandbox.md)
读取当前或指定 profile 等配置中的地域，生成 `https://api.<region>.e2b.fc.aliyuncs.com`。
支持地域从 FCSandbox 公共端点元数据动态查询；缺少地域、配置读取失败、
地域查询失败或地域不受支持时回退杭州。
例如北京地域的 FC 端点可配置为：

```bash
export E2B_API_URL="https://api.cn-beijing.e2b.fc.aliyuncs.com"
export E2B_DOMAIN="cn-beijing.e2b.fc.aliyuncs.com"
# E2B_API_KEY 使用对应 FC 项目的 API Key。
ecctl sbx template list --limit 20
```

识别依据是最终生效的 **API URL 主机名**，`E2B_DOMAIN` 不会覆盖一个已明确
配置的 API URL。主机名忽略大小写和末尾的点；主机名本身，或者从该主机名
出发的连续 CNAME 链中，出现 `e2b.fc.aliyuncs.com` 或其子域名时，模板列表
走 FC 路径。匹配要求完整 DNS 标签边界，不匹配相似字符串或无关 DNS 记录。

[FC 自定义域名](https://help.aliyun.com/zh/functioncompute/custom-domain-name)
可以将 `api.example.com` CNAME 到 FC API 域名。检测会保留链中的中间节点，
即使 FC 域名继续 CNAME 到不含 FC 后缀的负载均衡域名，也能识别。
检测仅决定 API 路径，不改写请求 URL、Host、TLS SNI 或 API Key 的发送目标。

DNS 查询使用系统配置，最多跟随 8 跳，总预算 3 秒。macOS 读取系统分域 DNS
路由，Linux 使用系统 resolv.conf，Windows 使用系统 DNS API；无需配置公共
DNS，也不依赖 `dig`。成功的检测结果仅缓存在当前 Caller 中；超时、循环、
解析失败不会被缓存。API 主机未被确认为 FC 时继续使用原生路径，HTTP 错误
不会触发回退；失败详情包含未识别原因。仅提供 A/AAAA、隐藏 CNAME 的代理
或 DNS flattening 无法据此自动识别 FC，可设置 `ECCTL_SANDBOX_BACKEND=fc`
显式选择兼容路径，或配置可验证的 FC API 主机名。

FC 列表按 `templateID` 排序后在本地分页，`--limit` 范围为 1–100，默认 100。
返回的 `pagination.next_token` 带有版本和端点指纹，下一页可直接传给
`--next-token`。FC token 与原生 token、不同端点的 FC token 均不可混用；
切换端点后请去掉旧 token 重新查询。

每一页都重新获取完整模板数组，因此每页传输和内存开销为 O(N)，排序为
O(N log N)。分页不是快照：并发新增且 ID 排在游标之前的模板不会出现在后续页。
返回非数组、重复 JSON 字段、尾随 JSON 内容、重复或空的 `templateID`、
服务端分页 token 时会明确报错，避免
将不完整响应误报为完整列表。

此适配仅改变模板列表路径。模板创建、更新及其他沙箱操作继续使用各自的
既有协议映射；它们在 FC 上的支持情况需要逐项验证。

## `ecctl sandbox template publish`

将 template 设置为公开。

## `ecctl sandbox template unpublish`

将 template 设置为私有。

## `ecctl sandbox template build-status`

读取指定 build 的状态、日志摘要和失败原因。

## `ecctl sandbox template build-logs`

分页读取指定 build 的结构化日志。

## `ecctl sandbox template tag-list`

列出 template build 的标签映射。

## `ecctl sandbox template tag-assign`

为 template build 分配一个或多个标签。

## `ecctl sandbox template tag-delete`

删除 template 的一个或多个标签。

E2B CLI 的 `template init` 和 `template migrate` 只生成或迁移本地工程文件，
不读取或变更远端 template 资源，因此不进入 ecctl 资源操作面。

## ACS 兼容模式

连接配置沿用 sandbox 资源的 `ECCTL_SANDBOX_BACKEND=acs`、端点、密钥和
可选 CA 文件。ACS 的模板来自已有 SandboxSet；`get` 使用原接口，
`list` 使用 `GET /templates` 返回完整数组，再按 templateID 排序、本地分页。
limit 默认 100、范围 1–100。重复字段、缺失/重复 ID、非数组、尾随 JSON
和服务端分页 token 均报错。每页需要 O(N) 读取和 O(N log N) 排序；
游标不提供一致性快照。

ACS 游标使用 `ecctl:acs-templates:v1:` 前缀并绑定 API 端点；FC 的既有
`ecctl:fc-templates:v1:` 格式不变。不同后端和端点的游标不可混用。
显式后端优先于域名检测，HTTP 失败不触发协议回退。

`delete` 允许删除快照模板；服务端明确拒绝 SandboxSet 模板时，返回
`UnsupportedACSTemplateDeletion`，保留原始错误及请求 ID，不提示更换密钥。
真正的认证失败仍按原认证错误返回。

ACS 模式下 create、update、publish、unpublish、build-status、build-logs、
tag-list、tag-assign、tag-delete 返回 `UnsupportedOperation`。
其他后端的构建状态、日志和标签接口在字段映射之前验证响应契约；
缺失必需字段、类型错误、错误构建 ID 或返回模板详情不能归一化为成功空结果。
合法的空日志/标签数组继续成功。GetSandbox 同样要求有效 sandboxID，避免
删除等待将错误结构的 HTTP 200 误判为资源不存在。

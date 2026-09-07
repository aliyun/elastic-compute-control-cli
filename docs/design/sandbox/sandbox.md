# sandbox sandbox

资源：E2B sandbox 实例

优先级：P1

`sandbox` 是独立产品名，`sbx` 是产品别名。默认资源同样名为
`sandbox`，所以 `ecctl sandbox list`、`ecctl sbx list` 和显式形式
`ecctl sandbox sandbox list` 指向同一资源。它与阿里云 AgentRun 的
`ecctl agentrun sandbox` 不是同一产品或 API。

该资源是全局资源，不解析或要求阿里云地域。请求使用 E2B 的标准
`E2B_API_KEY` 项目密钥和 `X-API-Key` 请求头。默认端点是
`https://api.e2b.app`；自托管环境可使用 `E2B_DOMAIN` 或
`E2B_API_URL`。自定义 URL 必须使用 HTTPS，仅允许字面量环回地址使用
HTTP。

官方契约：

- [E2B CLI](https://docs.e2b.dev/cli)
- [E2B OpenAPI](https://github.com/e2b-dev/E2B/blob/main/spec/openapi.yml)

## 资源操作

| ecctl 操作 | E2B API | 说明 |
| --- | --- | --- |
| `create` | `POST /sandboxes` | 从 template ID 或 alias 创建；默认等待状态为 `running`。 |
| `get` | `GET /sandboxes/{sandboxID}` | 返回状态、规格、过期时间、元数据和挂载信息。 |
| `list` | `GET /v2/sandboxes` | 支持状态、template、开始时间、排序和 metadata 过滤以及 token 分页。 |
| `update` | `POST .../timeout`、`PUT .../network` | 更新 TTL 和/或网络配置，并回读详情。 |
| `delete` | `DELETE /sandboxes/{sandboxID}` | E2B CLI 的 `kill` 作为该操作的别名。 |
| `pause` | `POST .../pause` | 默认等待状态为 `paused`；可选择是否保留内存。 |
| `resume` | `POST .../connect` | 仅执行控制面恢复/续期并等待 `running`，不打开终端。 |
| `refresh` | `POST .../refreshes` | 延长沙箱生命周期并回读详情。 |
| `fork` | `POST .../fork` | 创建一个或多个分叉，逐项保留成功结果或错误。 |
| `logs` | `GET /v2/.../logs` | 读取结构化 sandbox 日志。 |
| `metrics` | `GET .../metrics` | 读取 CPU、内存和磁盘指标。 |
| `snapshot` | `POST .../snapshots` | 从当前 sandbox 创建持久 snapshot template。 |

## `ecctl sandbox create`

创建 sandbox，并在未指定 `--no-wait` 时等待 `running`。

## `ecctl sandbox update`

更新 timeout、network 或两者，随后回读资源详情。

## `ecctl sandbox delete`

终止 sandbox，默认等待 `get` 确认不存在，最长 300 秒；`--timeout` 可调整等待时间，
`--no-wait` 在删除请求被接受后立即返回。超时或查询失败不会输出删除完成。
兼容 E2B 的 `kill` 名称作为命令别名。

## `ecctl sandbox get`

按 sandbox ID 获取当前控制面状态和资源属性。

## `ecctl sandbox list`

列出 sandbox，支持状态、template、时间、排序和 metadata 过滤。

## `ecctl sandbox pause`

暂停 sandbox，并可控制是否保留内存状态。

## `ecctl sandbox resume`

通过控制面恢复 sandbox，不启动交互式终端。

## `ecctl sandbox refresh`

按指定秒数延长沙箱生命周期。

## `ecctl sandbox fork`

从现有 sandbox 创建一个或多个副本。

## `ecctl sandbox logs`

按 cursor、方向、级别和文本条件读取结构化日志。

## `ecctl sandbox metrics`

读取指定时间范围内的 CPU、内存和磁盘指标。

## `ecctl sandbox snapshot`

从运行中的 sandbox 创建持久 template snapshot。

`connect` 的交互式 SSH/终端体验和 `exec` 使用 envd 运行时协议，包含
TTY、流式 I/O 和运行时访问令牌生命周期，不是一个声明式资源变更。
本资源不会把这两个本地交互命令伪装成普通 REST 操作；这里的
`resume` 只对应 E2B 控制面的 connect/resume 语义。

## 输出和错误

E2B 顶层数组响应统一映射为 ecctl 的资源数组；`X-Next-Token`、
`X-Total-Running` 和请求 ID 进入标准分页/动作输出。404 映射为
`not_found`，429 和服务端 5xx 映射为可重试服务错误，认证错误给出检查
`E2B_API_KEY` 的恢复建议。错误和输出都不得包含 API Key。

## ACS 兼容模式

设置 `ECCTL_SANDBOX_BACKEND=acs` 使用 ACS sandbox-manager 的 E2B 控制面。
该配置可选值为 `auto`（默认）、`e2b`、`fc`、`acs`，显式选择优先；
`auto` 保留原来的 FC 主机名/CNAME 检测，不根据自定义域名或 HTTP 404
推断 ACS。端点仍优先使用 `E2B_API_URL`，否则使用
`https://api.${E2B_DOMAIN}`，域名默认 `e2b.app`。选择后端不会改变请求目标。

ACS 的 `E2B_API_KEY` 对应 sandbox-manager 的管理密钥。CLI 不读取
kubeconfig，也不从 Deployment/Secret 自动提取密钥。私有 CA 可通过
`ECCTL_SANDBOX_CA_FILE` 指定 PEM 文件，追加到当前客户端的系统信任池；
保持域名验证、系统代理及禁止携带密钥跟随重定向的行为。
未设置 CA 文件时保持系统信任；无效配置在请求前失败。

当前兼容模式支持 create/get/list/delete、TTL update、pause/resume、
snapshot 和从快照 ID 创建。网络更新、logs、metrics、refresh、fork
返回 `UnsupportedOperation`。create 的 network、volume_mounts 参数同样
不受支持；其他高级参数未逐项实测，不应由基础生命周期通过推断其生效。
一条 update 同时携带 TTL 和 network 时，首次写请求之前即拒绝整条操作。

快照 ID 按 API 返回值原样传递，不假定它等于 Kubernetes Checkpoint 名称。
快照创建时长依赖集群驱动；本地客户端不会因超时自动重试创建。
删除完成指 API 已确认不存在，Kubernetes 对象的异步回收由 E2E 单独验证。

从快照启动可能超过一分钟。经过 ALB 的请求还受监听器超时限制，
`--timeout 300s` 仅扩大 CLI 的等待预算，不能延长网关预算。
ACS 的 [AlbConfig 监听器配置](https://help.aliyun.com/zh/cs/user-guide/alb-ingress-configuration-dictionary)
中 `spec.listeners[].requestTimeout` 默认 60 秒；集群管理员应根据恢复耗时
配置请求及空闲超时，并重新验证 HTTPS 恢复流程。排障时可通过受信任的
Kubernetes port-forward 访问管理服务，以区分入口超时和恢复失败。
收到 504 后创建结果仍可能不确定，应先按本次创建的 metadata 查询资源，
确认状态并清理；不要直接重复创建。

[ACS 官方兼容范围](https://help.aliyun.com/zh/cs/user-guide/connect-to-agent-sandbox-using-the-e2b-sdk)
及 [OpenKruise 快照约定](https://openkruise.io/kruiseagents/user-manuals/checkpoint)
是兼容依据；部署版本差异以对应版本的实测证据为准。
独立 ACS 用例和证据规范位于 `e2e/compat/acs/`。

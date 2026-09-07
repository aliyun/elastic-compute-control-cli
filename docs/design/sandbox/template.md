# sandbox template

资源：E2B sandbox template

优先级：P1

命令路径为 `ecctl sandbox template`，也支持产品别名 `sbx` 和资源别名
`tpl`。该资源与 `sandbox/sandbox` 使用相同的全局端点、
`E2B_API_KEY` 和安全策略。

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
`https://api.${E2B_DOMAIN}`，其中 `E2B_DOMAIN` 默认是 `e2b.app`。
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
或 DNS flattening 无法据此自动识别 FC，应配置可验证的 FC API 主机名。

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

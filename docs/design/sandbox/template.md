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
| `list` | `GET /v2/templates` | token 分页列出 template。 |
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

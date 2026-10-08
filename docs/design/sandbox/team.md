# sandbox team

资源：FCSandbox Team（团队）。命令路径为 `ecctl sandbox team`，使用
阿里云 profile 的 AK / STS 凭证和目标地域的 FCSandbox 端点。
沿用现有 `sandbox` 产品和 `sbx` 别名。Team 的 provider 为 `aliyun`；
现有 sandbox/template 的 provider 仍为 `e2b`，由资源 spec 明确选择，
没有按凭证或环境变量自动切换 Team 后端。
API 产品为 `FCSandbox`，版本 `2026-05-09`，协议 ROA。

## 产品路由、凭证与地域

| 资源入口 | 传输与凭证 | 地域/端点 |
|---|---|---|
| `sandbox team` / `sbx team` | FCSandbox ROA，Aliyun profile AK/STS | 必须通过 flag、环境或 profile 解析目标地域；使用 `fcsandbox.<region>.aliyuncs.com` |
| `sandbox` 默认资源 / `sbx` | 现有 E2B API，`E2B_API_KEY` | 仍为 global；`--region` 只提示默认 FC E2B 端点 |
| `sandbox template` / `sbx template` | 现有 E2B API，`E2B_API_KEY` | 保留 E2B URL/domain 及现有默认地域规则 |

`E2B_API_URL`、`E2B_DOMAIN`、`ECCTL_SANDBOX_BACKEND` 和沙箱 CA 设置
继续控制 E2B sandbox/template；不影响 Team 的 OpenAPI 目标。
Team 不接受 E2B API Key 作为 AK/STS 的替代品。默认 `sandbox` 简写仍指向
原来的 sandbox 资源；`team` 只通过明确的子资源路径访问。

## `ecctl sandbox team create`

`ecctl sandbox team create --name dev --description development --region cn-hangzhou`
调用 `POST /pop/2026-05-09/teams`（CreateTeam），直接返回该响应中已校验的团队字段。
不在成功创建后执行必须成功的 GetTeam 回读，确保立即保留 `team.id`，
让 E2E 捕获和登记删除 finalizer；后续查询失败不会触发重放已成功的创建。
需要刷新详情时显式使用 `get`。
支持 `--name`、`--description`、`--resource-group`、`--plan`，分别映射
`teamName`、`description`、`resourceGroupID`、`plan`。名称必填；不预设订阅计划。

## `ecctl sandbox team update`

`ecctl sandbox team update <team-id> --description updated`
先用 GetTeam 检查权限，再调用 `PUT /pop/2026-05-09/teams/{teamID}`
（UpdateTeam），最后回读团队。至少提供一个可修改字段：
`--name`、`--description`、`--resource-group`、`--plan`。
`readOnly=true` 时拒绝修改；名称发生变化而 `allowUpdateTeamName=false` 时
拒绝改名。权限字段缺失、类型错误或返回其他团队时失败，不发送修改请求。
预检查不能锁定远端资源，并发权限变化仍由服务端裁决；保留原始错误信息。
API schema 接受资源组归属，但[官方控制台说明](https://help.aliyun.com/en/agent-sandbox/user-guide/team-and-subscription-plans)
指出控制台不支持迁移已有团队的资源组；API 实际可修改范围仍需独立 live 验收。

## `ecctl sandbox team delete`

`ecctl sandbox team delete <team-id> --timeout 300s`
先检查 `readOnly`，再调用 `DELETE /pop/2026-05-09/teams/{teamID}`
（DeleteTeam），默认每 2 秒用 GetTeam 检查直到明确不存在。
`deleting` 继续等待；`delete_failed` 返回失败；超时或查询错误返回非零。
只有确认不存在后才输出 `deleted=true`。

`--no-wait` 只输出 `deletion_requested=true` 和团队 ID，保留 DeleteTeam
的请求 ID，不声称删除完成。DeleteTeam 的 HTTP 200 响应仅包含
`code/message/requestId`；元数据的 synchronous 标记不足以证明资源已清理。
官方 Team schema 定义 `active`、`deleting`、`delete_failed`，但未明确描述
GetTeam 的最终不存在响应。实现只接受 GetTeam 的 TeamNotFound / 404
作为不存在信号，拒绝将缺失 team 的成功响应当作不存在；这个转换需要
后续授权的 live 验收确认。其他资源的 NotFound 业务错误不能证明团队删除。

[官方说明](https://help.aliyun.com/en/agent-sandbox/user-guide/team-and-subscription-plans)
指出团队仍有 API Key 时不能删除。命令不自动清理 Team 内的 API Key、模板、
沙箱或卷；服务端拒绝时透传业务 code、message 和 requestId。
E2E 使用唯一团队名、显式 teardown 与本轮 cleanup journal；Team 不提供
基于标签的全账户 sweep 契约。

## `ecctl sandbox team get`

`ecctl sandbox team get <team-id>` 调用 GetTeam。
统一输出字段：`id`、`name`、`description`、`resource_group`、`plan`、
`status`、`created_time`、`user_id`、`allow_update_team_name`、`read_only`。
对应 `teamID`、`teamName`、`description`、`resourceGroupID`、`plan`、
`status`、`createdTime`、`userID`、`allowUpdateTeamName`、`readOnly`。

## `ecctl sandbox team list`

`ecctl sandbox team list --filter name=dev --filter plan=eco --limit 50 --page 1`
调用 ListTeams；筛选项为 `name`、`resource-group`、`plan`，分别映射
`teamName`、`resourceGroupID`、`plan`。计划筛选值由服务端校验（eco/std/pro）。
分页发送 `pageNumber/pageSize`，使用响应 `total`。`--limit` 默认 50，范围
1–50；`--page` 从 1 开始。输出保留 `total`、`pagination.has_more/next_page`。

`--all` 从第一页开始聚合全部结果，并保留所有筛选项。
空的中间页、重复 ID、变化的 total 或不完整分页响应返回错误。
它不提供一致性快照；并发团队变化可能需要重新查询。不得与非 1 页同时使用。

## 响应与覆盖

所有五个操作均校验小写业务 envelope，只有 `code="200"` 成功；
HTTP 200 的业务失败保留 `message` 和 `requestId`。详情响应必须包含匹配的
团队 ID 和状态；列表必须包含合法的 teams、页码、每页数量和 total。
格式错误不能归一化为成功空列表或删除完成。

离线测试覆盖 API 请求构建、字段映射、全量分页、只读/改名限制、业务失败、
删除失败、等待超时和 no-wait。E2E case 为 `e2e/cases/sandbox/team-lifecycle.yaml`；
新增五个操作在 registry 标为 offline/not-run，待独立授权的 live 验收。

依据：[FCSandbox 2026-05-09 官方元数据](https://api.aliyun.com/meta/v1/products/FCSandbox/versions/2026-05-09/api-docs.json)，
[CreateTeam](https://api.aliyun.com/document/FCSandbox/2026-05-09/CreateTeam)、
[GetTeam](https://api.aliyun.com/document/FCSandbox/2026-05-09/GetTeam)、
[ListTeams](https://api.aliyun.com/document/FCSandbox/2026-05-09/ListTeams)、
[UpdateTeam](https://api.aliyun.com/document/FCSandbox/2026-05-09/UpdateTeam)、
[DeleteTeam](https://api.aliyun.com/document/FCSandbox/2026-05-09/DeleteTeam)。

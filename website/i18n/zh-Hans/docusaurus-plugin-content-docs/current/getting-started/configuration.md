---
title: 配置
description: 配置 profile、凭证、地域、语言和输出。
---

# 配置

`ecctl configure` 写入资源命令使用的 ecctl 本地配置。原生 OAuth 登录只在其中保存非敏感 profile 元数据，包括经过验证的账号 ID；access token、refresh token 和换取到的 STS 凭证只进入 `~/.ecctl/credentials-v2/` 下的 canonical 私有存储。普通云命令仍会把本地 `aliyun` CLI profile 作为只读的回退来源。

## 配置地域

```bash
ecctl configure set region cn-hangzhou
```

输出形态：

```json
{
  "key": "region",
  "profile": "default",
  "sensitive": false,
  "value": "cn-hangzhou"
}
```

设置默认输出模式：

```bash
ecctl configure set output json
```

读取当前生效的 profile：

```bash
ecctl configure get
```

输出形态：

```json
{
  "lang": "",
  "mode": "",
  "output": "json",
  "profile": "default",
  "region": "cn-hangzhou"
}
```

## 凭证

`ecctl` 支持阿里云 CLI 当前的同组十一种凭证模式，并把兼容 `~/.aliyun/config.json` 的 profile 作为只读的回退来源。只有 `OAuth` 提供 `ecctl` 交互式配置流程：

```bash
ecctl configure --mode OAuth --profile production
ecctl --profile production ecs instance list --region cn-hangzhou
```

其余模式通过 `aliyun configure --mode <Mode>`、环境变量，或直接向兼容配置文件写入 profile 来配置。本地 AK 或 STS 凭证也可以直接设置：

```bash
ecctl configure set access-key-id <id>
ecctl configure set access-key-secret <secret>
ecctl configure set security-token <token>
```

[身份凭证](../credentials/index.md)是这一领域的参考文档：模式选择、两个配置文件及其能力差异、profile 与凭证解析顺序、身份固定、验证方式，以及 `DEBUG=dara` 的失败退出规则。每种凭证模式都有独立页面，可以从该总览或侧边栏进入。

## 支持的配置项

列出支持的配置项：

```bash
ecctl configure list
```

当前配置项：

| 配置项 | 存储为 | 取值 |
|---|---|---|
| `region` | `region_id` | 任意语法合法的阿里云地域 ID |
| `access-key-id` | `access_key_id` | 字符串 |
| `access-key-secret` | `access_key_secret` | 字符串，敏感 |
| `security-token` | `sts_token` | 字符串，敏感 |
| `lang` | `language` | `en`、`zh-CN` |
| `output` | `output_format` | `json`、`text` |
| `telemetry.enabled` | `telemetry.enabled` | `true`、`false`；所有 profile 共用，默认为 `true` |

敏感值默认掩码。仅在确实需要查看本地敏感值时使用 `--show-secret`。

## 产品遥测

产品遥测默认开启，并尽力发送到 ecctl 的 ARMS 采集端。STS `GetCallerIdentity` 成功解析支持的身份后，命令根 span 会包含以下原始标识：

| 字段 | 值 |
|---|---|
| `ecctl.identity.account_id` | `Account`、`RAMUser` 和 `AssumedRoleUser` 身份所属阿里云账号的 `AccountId` |
| `ecctl.identity.user_id` | RAM `UserId`，仅用于 `RAMUser` 身份 |
| `ecctl.identity.role_id` | RAM `RoleId`，仅用于 `AssumedRoleUser` 身份 |

这些字段直接使用 STS 返回的原始值，不做哈希处理，且只出现在命令根 span 上，不会附加到每个 API span。新增字段复用身份响应，不增加凭证采集。现有的 `ecctl.identity.hash` 和 `ecctl.identity.type` 字段继续保留，以兼容已有统计。

账号统计和身份主体统计回答不同的问题。例如，`account_id=A, user_id=U` 的 RAM 用户和 `account_id=A, role_id=R` 的角色，代表一个所属账号、两个身份主体。账号 ID 表示身份所属的账号，不代表每条命令都由该账号的主账号身份执行。历史遥测记录如果只保存了身份哈希，就无法补回这些原始 ID。

云服务返回 RequestId 时，云 API span（`ecctl.cloud.api.request`）会记录 `ecctl.cloud.request_id`。每次 OpenAPI 调用尝试，包括成功、失败和 ecctl 可见的重试，都会在各自的 span 上记录本次返回的 RequestId。同一条 trace 中，`ecctl.cloud.operation_id` 将同一操作的各次尝试归组，`ecctl.cloud.attempt` 标明这是第几次尝试。通过 `traceId` 可以将这些 API span 与命令根 span 上的原始账号、用户或角色 ID 关联。

RequestId 优先取自本次调用尝试的平台响应头（`X-Acs-Request-Id`、`X-Oss-Request-Id` 或 `X-Fc-Request-Id`）。否则，ecctl 仅接受随包 OpenAPI 响应定义声明的顶层字符串 ID 字段，或 API 契约内的结构化错误字段。二进制响应、事件流、不透明响应和定义未知的响应均不扫描正文，也不从非结构化 CLI 输出或错误文本中解析 ID。

网络失败、缺少 ID 或无法确认 ID 来源时不记录该属性。这可能降低 RequestId 覆盖率，以避免采集业务数据。被敏感信息脱敏规则移除的 ID 也会省略，不再尝试其他来源。遥测不采集请求体、响应体或错误文本。

ossutil 仅接受最终可见结果中的结构化顶层 ID，不采集纯文本诊断中的 ID。其 `ecctl.cloud.retry_observable=false`，内部重试对 ecctl 不可见。历史遥测记录未保存的 RequestId 无法补回。

通过任一环境变量关闭单条命令的遥测：

```bash
ECCTL_DISABLE_TELEMETRY=1 ecctl ecs instance list --region cn-hangzhou
DO_NOT_TRACK=1 ecctl ecs instance list --region cn-hangzhou
```

也可以持久关闭所有 profile 的遥测：

```bash
ecctl configure set telemetry.enabled false
```

## Profile

用 `--profile` 写入命名 profile：

```bash
ecctl --profile production configure set output json
```

profile 存在后切换当前 profile：

```bash
ecctl configure use production
```

`configure use` 会在兼容 `aliyun` 的配置和 `ecctl` 配置中检查该 profile 名，然后将所选 profile 记录到 `ecctl` 配置文件。

凭证 profile 的选择优先级如下：

1. `--profile`
2. `ECCTL_PROFILE`，然后是兼容的阿里云 profile 环境变量
3. 本地配置中的当前 profile

所选 profile 优先于普通凭证环境变量。设置 `ALIBABA_CLOUD_IGNORE_PROFILE=TRUE` 后，命令忽略已存储凭证，仅使用环境变量提供的凭证。显式选择的 profile 不存在时会直接失败，不会静默切换身份。

## 全局覆盖

全局 flag 可对单条命令覆盖配置：

```bash
ecctl --region cn-beijing --output json --lang en schema --list ecs
```

常用全局 flag：

| Flag | 用途 |
|---|---|
| `--profile` | 选择配置 profile |
| `--region` | 选择当前命令的阿里云地域 |
| `--output` | 选择 `json` 或 `text` 输出 |
| `--json` | 强制 JSON 输出 |
| `--lang` | 选择 `en` 或 `zh-CN` 文案 |
| `--no-color` | 关闭人类可读输出的颜色 |
| `--agent-envelope` | 将 JSON 输出包裹在 ecctl Agent envelope 中 |

## 环境变量

`ecctl` 识别以下环境变量覆盖：

| 变量 | 用途 |
|---|---|
| `ECCTL_PROFILE`、`ALIBABACLOUD_PROFILE`、`ALIBABA_CLOUD_PROFILE`、`ALICLOUD_PROFILE` | 未传 `--profile` 时的默认 profile |
| `ECCTL_REGION`、`ALIBABA_CLOUD_REGION_ID`、`ALIBABACLOUD_REGION_ID`、`ALICLOUD_REGION_ID` | 未传 `--region` 时的地域覆盖 |
| `ECCTL_ALIYUN_CONFIG_PATH`、`ALIBABA_CLOUD_CONFIG_PATH`、`ALIBABACLOUD_CONFIG_PATH`、`ALICLOUD_CONFIG_PATH` | 兼容 `aliyun` CLI 的配置文件路径，按此顺序检查 |
| `ALIBABA_CLOUD_IGNORE_PROFILE` | 设为 `TRUE` 时忽略已存储的凭证 profile |
| `ALIBABA_CLOUD_ACCESS_KEY_ID`、`ALIBABA_CLOUD_ACCESS_KEY_SECRET`、`ALIBABA_CLOUD_SECURITY_TOKEN` | AK 或 STS 凭证 |
| `ALIBABA_CLOUD_ROLE_ARN`、`ALIBABA_CLOUD_ROLE_SESSION_NAME`、`ALIBABA_CLOUD_EXTERNAL_ID` | RAM 角色扮演 |
| `ALIBABA_CLOUD_ECS_METADATA`、`ALIBABA_CLOUD_IMDSV1_DISABLED` | ECS 实例 RAM 角色和 IMDS 策略 |
| `ALIBABA_CLOUD_OIDC_PROVIDER_ARN`、`ALIBABA_CLOUD_OIDC_TOKEN_FILE` | OIDC/RRSA 凭证 |
| `ALIBABA_CLOUD_CREDENTIALS_URI` | CredentialsURI 端点 |
| `ALIBABA_CLOUD_BEARER_TOKEN`、`ALIBABA_CLOUD_BEARER_TOKEN_HEADER_KEY` | Bearer 令牌和可选的自定义请求头 |
| `ALIBABA_CLOUD_DISABLE_EXTERNAL_PROCESS` | 禁用 `External` 和 `CredentialsURI` 凭证来源 |
| `ECCTL_DISABLE_TELEMETRY`、`DO_NOT_TRACK` | 任一变量设为 `1` 时关闭产品遥测 |

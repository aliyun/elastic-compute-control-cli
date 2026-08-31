---
title: 配置
description: 配置 profile、凭证、地域、语言和输出。
---

# 配置

`ecctl configure` 写入资源命令使用的 ecctl 本地配置。原生 OAuth 登录只在其中保存非敏感 profile 元数据，包括经过验证的账号 ID；access token、refresh token 和换取到的 STS 凭证只进入 `~/.ecctl/credentials-v2/` 下的 canonical 私有存储。普通云命令仍会把本地 `aliyun` CLI profile 作为只读 fallback。

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

读取 `~/.aliyun/config.json` 时，`ecctl` 支持阿里云 CLI 当前的同组凭证模式：

| 模式 | 典型场景 |
|---|---|
| `OAuth` | 浏览器登录的本地用户；缓存令牌可自动刷新 |
| `EcsRamRole` | 通过 IMDS 使用 ECS 实例 RAM 角色 |
| `RamRoleArn` | 从 AK 或 STS 源凭证扮演 RAM 角色 |
| `ChainableRamRoleArn` | 通过命名的源 profile 形成角色链 |
| `OIDC` | OIDC/RRSA 工作负载身份 |
| `CloudSSO` | CloudSSO 访问配置 |
| `External` | 不经过 shell，按 argv 执行凭证辅助程序 |
| `CredentialsURI` | 从 HTTPS 或 loopback HTTP 端点获取可续期 STS 凭证 |
| `StsToken` | 已有的临时 AK、Secret 和 SecurityToken |
| `BearerToken` | 接受 Bearer 鉴权的产品 API |
| `AK` | 长期 AccessKey 凭证 |

OAuth 登录沿用阿里云 CLI 的常用命令形态，可以直接由 `ecctl` 完成：

```bash
ecctl configure --mode OAuth --profile production
ecctl --profile production ecs instance list --region cn-hangzhou
```

默认站点为 `CN`。需要国际站 OAuth 服务或指定 ecctl 元数据配置文件时使用：

```bash
ecctl configure --mode OAuth --profile production --oauth-site-type INTL
ecctl configure --mode OAuth --profile production --config-path /path/to/config.json
```

在非交互终端中，或者已经知道目标账号时，请把登录绑定到预期的 16 位阿里云账号：

```bash
ecctl configure --mode OAuth --profile production --expected-account-id 1234567890123456
```

首次交互登录会显示经过验证的账号 ID，并要求输入完整 ID 后才保存凭证。后续登录必须与该 profile 已记录的账号一致；需要有意切换账号时，显式传入新的 `--expected-account-id`。

后续资源命令需要使用自定义路径时，请导出 `ECCTL_CONFIG_PATH=/path/to/config.json` 并选择同名 profile。原生 OAuth 的 config path 不能与 aliyun CLI 配置路径相同。

登录使用 PKCE，并且 HTTP 回调只监听 `127.0.0.1` 的 12345 到 12349 端口。自动打开浏览器成功时不会输出一次性授权地址；需要手工打开时，请在私有终端中使用 `--manual`，不要把地址复制到共享日志。成功时 stdout 只包含 profile、模式、站点、经过验证的账号 ID、配置路径和浏览器启动状态，不包含 token。浏览器启动进程不会继承阿里云或 OSS 凭证环境变量。CloudSSO 等其他需要浏览器的高级模式仍由阿里云 CLI 配置。

`ecctl` 会在整条命令期间保留选定的凭证 provider，并在后续请求签名前按需刷新临时凭证。首个可续期凭证会固定规范化的账号、用户或角色；后续凭证如果属于其他身份，会在签名业务请求前被拒绝。原生 OAuth 身份过期时重新执行 `ecctl configure --mode OAuth`；Aliyun-compatible OAuth 仍使用 `aliyun configure` 重新认证。命令运行期间如果所选 profile 的身份字段发生变化，`ecctl` 会失败退出，不会在同一条命令中切换账号。

RAM 角色和 OIDC profile 必须使用完整的 `acs:ram::<16位账号ID>:role/<角色名>` ARN。`ecctl` 从 ARN 派生预期账号，并在第一条业务请求前通过官方 STS `GetCallerIdentity` 端点验证初始凭证。显式 custom `sts_endpoint` 可以签发凭证，但不能验证自己签发的结果。独立身份检查需要使用地域或 VPC STS 端点时，请设置 `sts_region` 和 `enable_vpc`。

云命令只读兼容的 `aliyun` 配置。同名的 ecctl 原生 OAuth 元数据优先于 Aliyun profile；不存在原生元数据时，原有 Aliyun profile 继续可用。轮换后的 OAuth token，以及 OAuth/CloudSSO 的 STS 缓存，按 profile 分别写入 `~/.ecctl/credentials-v2/`，目录和文件仅当前用户可访问。该存储使用当前进程解析到的 home，不随 `ECCTL_CONFIG_PATH` 改变。原生 OAuth 每个 profile 只有一个 per-user owner，新的 login generation 会让旧元数据失效，而不会静默切换身份；Aliyun-compatible entry 仍按来源路径和 profile 名隔离。如果服务端 refresh token 轮换无法提交到本地，`ecctl` 会停止且不重试该轮换，profile 必须重新认证。

原生 OAuth cache 的每次写入都会在 per-profile 锁内比较 active generation。登录还会保留私有 write-ahead transaction，直到 cache 与 ecctl metadata 的 generation 一致。如果进程或主机在两次写入之间停止，下一次登录或凭证加载会先恢复旧 generation 或完成新 generation，再使用任何 token。

当 OSS 命令使用可续期凭证时，`ecctl` 通过仅绑定到 `127.0.0.1` 的短期凭证端点和仅当前用户可读的临时 profile，向本地 `ossutil` 子进程提供凭证。该端点使用每条命令独有的随机路径；子进程退出后，端点和临时 profile 都会立即删除，凭证不会出现在命令参数中。External 凭证获取的 deadline 为 60 秒；Unix 会在取消时终止整个进程组，所有平台都会在额外两秒宽限后强制释放继承的输出管道。无过期时间的 External AK 会作为本次 OSS 操作的静态 AK 使用；可续期 OSS broker 响应必须是包含 SecurityToken 的 STS 凭证。

上游 Dara 请求日志会在 `ecctl` 的最终 HTTP client 能够脱敏之前打印已签名 URL 和请求头。因此，逗号分隔的 `DEBUG` 环境变量包含精确 token `dara` 时，携带凭证的命令会失败退出。移除该 token 后再重试。

如需由 `ecctl` 管理本地 AK profile：

```bash
ecctl configure set access-key-id <id>
ecctl configure set access-key-secret <secret>
```

使用 STS 访问时，再设置安全令牌：

```bash
ecctl configure set security-token <token>
```

`StsToken` 凭证会直接使用，无法自行续期。profile 包含 `sts_expiration`
时，`ecctl` 会拒绝已过期的令牌，也会在令牌无法覆盖已知命令 deadline
时提前失败。长时间 OSS 传输应优先使用 OAuth、OIDC、RAM 角色、ECS
角色、External 或 CredentialsURI 等可续期模式。

`External` 和 `CredentialsURI` 会执行本地程序或访问外部端点。设置
`ALIBABA_CLOUD_DISABLE_EXTERNAL_PROCESS=true` 可同时禁用这两类来源。
External 命令只会拆分为 argv 后直接执行，不会交给 shell 求值。
CredentialsURI 必须使用 HTTPS；只有 `127.0.0.1`、`::1` 等字面量
loopback IP 可以使用 HTTP。

CredentialsURI 端点遵循阿里云 CLI 响应契约：必须返回 HTTP 200，JSON
中必须包含 `Code: "Success"`、`AccessKeyId`、`AccessKeySecret`、
`SecurityToken`，以及 RFC 3339 UTC 格式的 `Expiration`。

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

敏感值默认掩码。仅在确实需要查看本地敏感值时使用 `--show-secret`。

## Profile

用 `--profile` 写入命名 profile：

```bash
ecctl --profile production configure set output json
```

profile 存在后切换当前 profile：

```bash
ecctl configure use production
```

`configure use` 会在兼容的 `aliyun` 配置和 `ecctl` 配置中检查该 profile 名，然后将所选 profile 记录到 `ecctl` 配置文件。

凭证 profile 的选择优先级如下：

1. `--profile`
2. `ECCTL_PROFILE`，然后是兼容的阿里云 profile 环境变量
3. 本地配置中的当前 profile

所选 profile 优先于普通凭证环境变量。设置
`ALIBABA_CLOUD_IGNORE_PROFILE=TRUE` 后，命令忽略已存储凭证，仅使用环境变量提供的凭证。显式选择的 profile 不存在时会直接失败，不会静默切换身份。

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
| `ALIBABA_CLOUD_CONFIG_PATH`、`ALIBABACLOUD_CONFIG_PATH`、`ALICLOUD_CONFIG_PATH` | 兼容的 `aliyun` CLI 配置文件路径 |
| `ALIBABA_CLOUD_IGNORE_PROFILE` | 设为 `TRUE` 时忽略已存储的凭证 profile |
| `ALIBABA_CLOUD_ACCESS_KEY_ID`、`ALIBABA_CLOUD_ACCESS_KEY_SECRET`、`ALIBABA_CLOUD_SECURITY_TOKEN` | AK 或 STS 凭证 |
| `ALIBABA_CLOUD_ROLE_ARN`、`ALIBABA_CLOUD_ROLE_SESSION_NAME`、`ALIBABA_CLOUD_EXTERNAL_ID` | RAM 角色扮演 |
| `ALIBABA_CLOUD_ECS_METADATA`、`ALIBABA_CLOUD_IMDSV1_DISABLED` | ECS 实例 RAM 角色和 IMDS 策略 |
| `ALIBABA_CLOUD_OIDC_PROVIDER_ARN`、`ALIBABA_CLOUD_OIDC_TOKEN_FILE` | OIDC/RRSA 凭证 |
| `ALIBABA_CLOUD_CREDENTIALS_URI` | CredentialsURI 端点 |
| `ALIBABA_CLOUD_BEARER_TOKEN`、`ALIBABA_CLOUD_BEARER_TOKEN_HEADER_KEY` | Bearer Token 和可选自定义 Header |
| `ALIBABA_CLOUD_DISABLE_EXTERNAL_PROCESS` | 禁用 `External` 和 `CredentialsURI` 凭证来源 |

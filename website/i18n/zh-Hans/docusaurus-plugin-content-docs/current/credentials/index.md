---
title: 身份凭证
description: 选择、配置并验证 ecctl 接受的阿里云凭证模式。
---

# 身份凭证

`ecctl` 接受与阿里云 CLI 相同的十一种凭证模式，因此已有的 `aliyun`
profile 无需修改即可继续使用。本节说明每种模式在哪里配置、同时存在多种
凭证时 `ecctl` 如何选择其中一种，以及如何在命令签名请求之前确认所选
结果。

请先阅读本页，再打开你打算使用的模式对应的页面。

## 选择模式

| 模式 | 凭证生命周期 | 典型场景 | 页面 |
|---|---|---|---|
| `OAuth` | 可续期 | 本地用户通过浏览器登录 | [OAuth](./oauth.md) |
| `AK` | 静态 | 长期 AccessKey 凭证 | [AccessKey](./ak.md) |
| `StsToken` | 固定过期时间 | 在别处签发的临时令牌 | [STS 令牌](./sts-token.md) |
| `EcsRamRole` | 可续期 | 绑定 RAM 角色的 ECS 实例上的工作负载 | [ECS RAM 角色](./ecs-ram-role.md) |
| `RamRoleArn` | 可续期 | 从 AK 或 STS 源凭证扮演 RAM 角色 | [RAM 角色 ARN](./ram-role-arn.md) |
| `ChainableRamRoleArn` | 可续期 | 通过命名的源 profile 扮演 RAM 角色 | [链式 RAM 角色 ARN](./chainable-ram-role-arn.md) |
| `OIDC` | 可续期 | Kubernetes RRSA 或其他 OIDC 工作负载身份 | [OIDC](./oidc.md) |
| `CloudSSO` | 可续期 | 企业 CloudSSO 访问配置 | [CloudSSO](./cloudsso.md) |
| `External` | 取决于辅助程序 | 输出凭证的本地程序 | [External 进程](./external.md) |
| `CredentialsURI` | 可续期 | HTTPS 或 loopback HTTP 凭证端点 | [Credentials URI](./credentials-uri.md) |
| `BearerToken` | 静态 | 接受 Bearer 鉴权的产品 API | [Bearer 令牌](./bearer-token.md) |

长时间运行的操作应优先选择可续期模式。可续期凭证在临近过期时，会在
后续已签名请求之前完成刷新，因此持续数小时的 OSS 传输或长时间运行的
`ecctl` 脚本不会中途失败。`AK`、`StsToken` 和 `BearerToken` 无法自行
刷新。

## ecctl 如何为各模式写入配置

`ecctl` 能解析全部十一种模式，但并不会为其中每一种都提供交互式配置
流程。`ecctl configure --mode` 只接受 `OAuth`；其他任何取值都会被
拒绝：

```bash
ecctl configure --mode AK --profile production
```

```json
{
  "error": {
    "kind": "client",
    "code": "UnsupportedCredentialMode",
    "message": "credential mode AK is not supported by ecctl configure",
    "retryable": false,
    "accepted_values": ["OAuth"]
  }
}
```

因此，写入配置的路径取决于具体模式：

| 模式 | 写入配置的方式 |
|---|---|
| `OAuth` | `ecctl configure --mode OAuth` |
| `AK`、`StsToken` | `ecctl configure set access-key-id`、`access-key-secret`、`security-token`，或 `aliyun configure --mode AK` |
| `CloudSSO` | `aliyun configure --mode CloudSSO` |
| 其他所有模式 | `aliyun configure --mode <Mode>`、环境变量，或写入兼容 `aliyun` 配置文件的 profile |

## 配置文件

`ecctl` 读取两个配置文件，并把可续期状态写入
第三个位置。

| 路径 | 环境变量覆盖 | 作用 |
|---|---|---|
| `~/.ecctl/config.json` | `ECCTL_CONFIG_PATH` | ecctl 元数据：地域、语言、输出、当前 profile、原生 OAuth 登录状态，以及本地 AK 或 STS 凭证 |
| `~/.aliyun/config.json` | `ECCTL_ALIYUN_CONFIG_PATH`，然后是 `ALIBABA_CLOUD_CONFIG_PATH`、`ALIBABACLOUD_CONFIG_PATH`、`ALICLOUD_CONFIG_PATH` | 兼容的阿里云 CLI profile，云命令执行期间只读 |
| `~/.ecctl/credentials-v2/` | 无 | 轮换后的 OAuth 与 CloudSSO 令牌以及缓存 STS 凭证的私有存储 |

两个配置文件使用相同的 JSON 形态：

```json
{
  "current": "default",
  "profiles": [
    {
      "name": "default",
      "mode": "OAuth",
      "region_id": "cn-hangzhou"
    }
  ]
}
```

私有存储使用当前进程解析到的 home 目录，并且不随
`ECCTL_CONFIG_PATH` 改变。其中的条目以仅当前用户可访问的权限写入。
可续期的 OAuth 和 CloudSSO 状态绝不会写入 Aliyun
配置文件。

两个文件的能力存在两处差异，而且这种差异很关键：

- **Aliyun-compatible** 文件中的 profile 可以使用十一种模式中的任意一种。
- **ecctl** 文件中的 profile 只解析原生 `OAuth`（即带有 `mode: OAuth`
  且 `oauth_generation` 非空的 profile）或静态凭证（即带有
  `access_key_id`、`access_key_secret` 或 `sts_token` 的 profile）。

声明其他模式的 ecctl profile 不会通过该模式解析。如果 profile
同时带有静态凭证，`ecctl` 会直接使用这些凭证并忽略其余字段，因此
ecctl profile 上的 `ram_role_arn` 会静默失效。声明 `External`
或 `CredentialsURI` 却没有静态凭证的 profile 会被路由到环境变量凭证链：
环境中存在凭证时按环境凭证解析，只有环境同样没有凭证时才报
`MissingCredentials`。任何非静态、非 OAuth 的 profile 都应放在
Aliyun-compatible 文件中。

## 解析顺序

profile 选择按以下顺序进行：

1. `--profile`
2. `ECCTL_PROFILE`，然后是 `ALIBABACLOUD_PROFILE`、
   `ALIBABA_CLOUD_PROFILE`、`ALICLOUD_PROFILE`
3. 本地配置中的 `current` 值

确定 profile 名之后，凭证解析按以下顺序进行：

1. `ALIBABA_CLOUD_IGNORE_PROFILE=TRUE` 会短路其余全部步骤，只使用
   环境变量凭证。
2. 属于原生 OAuth 登录的 ecctl profile。
3. 带有静态凭证覆盖的 ecctl profile。
4. 同名的 Aliyun-compatible profile，通过完整的十一模式链
   解析。
5. 没有任何凭证的 ecctl profile 会 fallback 到环境变量凭证。
6. 显式请求的 profile，或已存储的 `current` profile，如果在两个文件中
   都找不到，会以 `ProfileNotFound` 失败。
7. 没有 profile 参与时，使用环境变量凭证。

有两点结论值得记住：

- 匹配到的 profile 绝不会切换到*另一种*凭证来源。声明了
  `CredentialsURI` 但自身没有 `credentials_uri`、环境中也没有
  `ALIBABA_CLOUD_CREDENTIALS_URI` 的 profile，会以
  `InvalidCredentials` 失败，而不会悄悄退化成碰巧导出的 access key 对。
- 但在自己的来源内部，匹配到的 profile 确实会用环境变量补齐空缺。
  `AK`、`StsToken` 和 `RamRoleArn` 分支对每个字段都是先取 profile
  的值、再取对应环境变量的值；profile 完全没有声明模式时，模式推断也走
  同样的顺序。因此一个声明了 `AK` 却把 `access_key_id` 和
  `access_key_secret` 留空的 profile，最终解析到的是环境变量里的 access
  key 对。`credentials_uri` 和 `bearer_token` 也以同样的方式回退到各自的
  环境变量。

选中一个 profile 并不会关闭环境变量凭证。如果你需要某次命令只使用
profile 自身携带的内容，请在该次调用前取消这些凭证环境变量。

### 环境变量凭证链

`ecctl` 使用环境变量凭证时，按以下顺序测试各个来源：

1. `ALIBABA_CLOUD_ACCESS_KEY_ID` 与 `ALIBABA_CLOUD_ACCESS_KEY_SECRET`；
   同时存在 `ALIBABA_CLOUD_SECURITY_TOKEN` 时变为 `StsToken`，同时存在
   `ALIBABA_CLOUD_ROLE_ARN` 时变为 `RamRoleArn`。只设置 access key
   对中的一个属于硬错误。
2. `OIDC`，要求 `ALIBABA_CLOUD_OIDC_PROVIDER_ARN`、
   `ALIBABA_CLOUD_OIDC_TOKEN_FILE` 和 `ALIBABA_CLOUD_ROLE_ARN` 三者全部存在。
3. 来自 `ALIBABA_CLOUD_ECS_METADATA` 的 `EcsRamRole`。
4. OIDC 变量只设置了一部分属于硬错误，而不是静默跳过。
5. 来自 `ALIBABA_CLOUD_CREDENTIALS_URI` 的 `CredentialsURI`。
6. 来自 `ALIBABA_CLOUD_BEARER_TOKEN` 的 `BearerToken`。
7. 否则为 `MissingCredentials`。

这条链上的前缀别名并不统一：

| 变量 | 接受的前缀 |
|---|---|
| `ACCESS_KEY_ID`、`ACCESS_KEY_SECRET`、`SECURITY_TOKEN` | `ALIBABA_CLOUD_`、`ALIBABACLOUD_`、`ALICLOUD_`，以及不带前缀 |
| `ROLE_ARN`、`OIDC_PROVIDER_ARN`、`OIDC_TOKEN_FILE`、`EXTERNAL_ID` | `ALIBABA_CLOUD_` 和 `ALIBABACLOUD_` |
| `ROLE_SESSION_NAME`、`STS_ENDPOINT`、`STS_REGION`、`VPC_ENDPOINT_ENABLED`、`ECS_METADATA`、`IMDSV1_DISABLED`、`CREDENTIALS_URI`、`BEARER_TOKEN`、`BEARER_TOKEN_HEADER_KEY` | 仅 `ALIBABA_CLOUD_` |

这个差别很容易踩到。`ALICLOUD_ROLE_ARN` 根本不会被读取，因此把它和
access key 对一起导出并不会选中 `RamRoleArn`。这对 key 会解析为普通的
`AK`，命令以 RAM 用户自身而不是所扮演的角色执行，并且没有任何警告。

## 验证所选凭证

`ecctl configure get` 报告当前生效的 profile，不会发起云
调用：

```bash
ecctl --profile production configure get
```

```json
{
  "lang": "",
  "mode": "OAuth",
  "output": "json",
  "profile": "production",
  "region": "cn-hangzhou"
}
```

这里报告的是 profile 声明的内容，并不能证明该凭证可以解析
成功。对于会访问网络的模式，请用一次开销很小的读操作
确认：

```bash
ecctl --profile production --region cn-hangzhou ecs region list
```

凭证问题会在任何资源操作执行之前，以 `InvalidCredentials`、
`MissingCredentials`、`ProfileNotFound` 或 `CredentialSourceDisabled`
客户端错误的形式出现。如果错误中点名了某个阿里云 API 并携带 Request ID，
说明凭证已解析、已完成签名并到达了服务端。

敏感值默认掩码。确实需要查看某个敏感值时，使用
`--show-secret`：

```bash
ecctl configure get access-key-secret --show-secret
```

`configure set` 把值当作位置参数接收，没有交互式输入。在命令行里直接敲出的
密钥会写入 shell history，并且在命令执行期间对本机其他进程通过 `ps`
可见。在共用机器上，优先使用 `OAuth`，或者把值直接写入配置文件。

## 身份固定

`ecctl` 会在整条命令期间保留选定的凭证 provider，并在需要时
于后续已签名请求之前刷新临时凭证。首个可续期凭证会固定规范化的账号、
用户或角色。属于其他身份的后续凭证会在它能够签名请求之前被拒绝，
因此轮换后的令牌无法在命令执行中途切换
账号。

`RamRoleArn`、`ChainableRamRoleArn` 和 `OIDC` profile 必须使用完整的
`acs:ram::<16位账号ID>:role/<角色名>` ARN。`ecctl` 会从该
ARN 派生预期账号，并在第一条业务请求之前通过官方 STS
`GetCallerIdentity` 端点验证初始凭证。自定义 `sts_endpoint`
可以签发凭证，但永远不会被信任用于验证它自己签发的结果。当这项独立的
身份检查必须使用地域或 VPC STS 端点时，请设置 `sts_region` 和
`enable_vpc`。

## 调试日志

上游 Dara 请求日志会在最终的 `ecctl` HTTP client 能够脱敏之前，打印
已签名的 URL 和请求头。因此，当逗号分隔的 `DEBUG` 环境变量包含精确
token `dara` 时，携带凭证的命令会失败退出。重试之前请移除该
token。

## 相关文档

- [配置](../getting-started/configuration.md)：地域、语言、
  输出和 profile 管理
- [环境变量](../getting-started/configuration.md#环境变量)：
  完整的覆盖项列表
- [错误模型](../reference/errors.md)：凭证失败如何报告

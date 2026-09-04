---
title: OIDC
description: 使用 ecctl 把 OIDC 令牌换取可续期的阿里云凭证。
---

# OIDC

`OIDC` 通过 STS `AssumeRoleWithOIDC`，把 OIDC 身份令牌换取为临时的阿里云凭证。任何位置都不保存阿里云密钥：OIDC 令牌文件是唯一的输入，并且通常由工作负载平台签发和轮换。

对于使用 RRSA 的 Kubernetes Pod、GitHub Actions、GitLab CI，或任何为其作业签发 OIDC 令牌的系统，这都是合适的凭证模式。凭证是可续期的，因此长时间操作会自行刷新。

## 使用阿里云 CLI 配置

```bash
aliyun configure --mode OIDC --profile rrsa
```

不支持 `ecctl configure --mode OIDC`。`--mode` 只接受 `OAuth`，并且 ecctl 原生 profile 只解析 OAuth 或静态凭证。请把该 profile 写入兼容 `aliyun` 的配置文件。

## 使用环境变量配置

```bash
export ALIBABA_CLOUD_OIDC_PROVIDER_ARN=acs:ram::1234567890123456:oidc-provider/ack-rrsa-c1234
export ALIBABA_CLOUD_OIDC_TOKEN_FILE=/var/run/secrets/tokens/oidc-token
export ALIBABA_CLOUD_ROLE_ARN=acs:ram::1234567890123456:role/pod-role
export ALIBABA_CLOUD_ROLE_SESSION_NAME=ecctl-session
```

provider ARN、令牌文件和 role ARN 三者必须同时具备。在环境变量链中，这一组的检测位于 AccessKey 密钥对之后、`ALIBABA_CLOUD_ECS_METADATA` 之前。

只填了一部分会直接报错，不会被静默跳过：

```json
{
  "error": {
    "kind": "client",
    "code": "InvalidCredentials",
    "message": "ALIBABA_CLOUD_OIDC_PROVIDER_ARN, ALIBABA_CLOUD_OIDC_TOKEN_FILE, and ALIBABA_CLOUD_ROLE_ARN are all required for OIDC credentials"
  }
}
```

这个顺序在实践中很重要。一个设置了 provider ARN 和 role ARN、但令牌文件缺失或未挂载的 Pod，会用这条消息失败，而不会悄悄落到实例元数据上。

这条路径同样会读取 `ALIBABA_CLOUD_STS_ENDPOINT`、`ALIBABA_CLOUD_STS_REGION` 和 `ALIBABA_CLOUD_VPC_ENDPOINT_ENABLED`。

## profile 字段

| 字段 | 必填 | 说明 |
|---|---|---|
| `mode` | 否 | `OIDC`。存在 `oidc_provider_arn` 时会被推断 |
| `oidc_provider_arn` | 是 | `acs:ram::<16位账号ID>:oidc-provider/<名称>` |
| `oidc_token_file` | 是 | 投射令牌的路径 |
| `ram_role_arn` | 是 | `acs:ram::<16位账号ID>:role/<角色名>` |
| `ram_session_name` | 否 | 未设置时回退到 `ALIBABA_CLOUD_ROLE_SESSION_NAME` |
| `expired_seconds` | 否 | 请求的会话有效期（秒） |
| `policy` | 否 | 进一步收窄所扮演会话的内联策略 |
| `sts_endpoint` | 否 | 自定义 STS 端点。必须使用 HTTPS |
| `sts_region` | 否 | 地域 STS 端点选择 |
| `enable_vpc` | 否 | 使用 VPC STS 端点 |

```json
{
  "name": "rrsa",
  "mode": "OIDC",
  "oidc_provider_arn": "acs:ram::1234567890123456:oidc-provider/ack-rrsa-c1234",
  "oidc_token_file": "/var/run/secrets/tokens/oidc-token",
  "ram_role_arn": "acs:ram::1234567890123456:role/pod-role",
  "ram_session_name": "ecctl-session",
  "region_id": "cn-hangzhou"
}
```

## 校验

三个字段必须同时具备：

```json
{
  "error": {
    "kind": "client",
    "code": "InvalidCredentials",
    "message": "oidc_provider_arn, oidc_token_file, and ram_role_arn are required for OIDC credentials"
  }
}
```

两个 ARN 都必须完整且格式正确，并且必须属于同一个账号：

```json
{
  "error": {
    "kind": "client",
    "code": "InvalidCredentials",
    "message": "OIDC provider ARN and RAM role ARN must belong to the same account"
  }
}
```

这些检查都在本地执行，并且发生在读取令牌文件之前。这正是同账号规则的意义：`ecctl` 会在读取或传输 OIDC 令牌之前校验派生出的 STS 端点，因此配置不匹配时，令牌不可能被出示给错误账号的端点。

与 [RAM 角色 ARN](./ram-role-arn.md) 相同，`ecctl` 从 role ARN 派生预期账号，并在第一条业务请求前通过官方 STS `GetCallerIdentity` 端点验证初始凭证。自定义 `sts_endpoint` 可以签发凭证，但永远不会被信任去验证自己签发的结果。

## 验证

```bash
ecctl --profile rrsa configure get
ecctl --profile rrsa --region cn-hangzhou ecs region list
```

请在工作负载内部执行该检查。在工作负载外部，投射的令牌文件通常并不存在，此时的失败也说明不了配置本身的任何问题。

## 续期与令牌轮换

扮演得到的凭证是可续期的，接近过期时 `ecctl` 会在后续已签名请求之前刷新。每次刷新都会重新读取令牌文件，因此会轮换投射令牌的平台在长时间操作中无需人工介入即可持续工作。

第一个凭证会固定规范化的账号和角色。属于不同身份的刷新凭证会在其签名请求前被拒绝。

## 相关文档

- [ECS RAM 角色](./ecs-ram-role.md)：没有 OIDC 的 ECS 上的工作负载
- [RAM 角色 ARN](./ram-role-arn.md)：从已存储的源凭证扮演 RAM 角色
- [身份凭证](./index.md)：解析顺序

---
title: RAM 角色 ARN
description: 从 AccessKey 或 STS 源凭证扮演 RAM 角色。
---

# RAM 角色 ARN

`RamRoleArn` 使用源凭证调用 STS `AssumeRole`，并把得到的临时凭证用于每一次已签名请求。适用场景包括：跨越账号边界、把宽泛的源身份收窄到特定角色，或满足要求扮演角色的 policy。

结果是可续期的，因此长时间操作可以持续运行：当扮演得到的凭证接近过期时，`ecctl` 会在后续已签名请求之前刷新。

## 使用阿里云 CLI 配置

```bash
aliyun configure --mode RamRoleArn --profile cross-account
```

不支持 `ecctl configure --mode RamRoleArn`。`--mode` 只接受 `OAuth`，而 ecctl 原生 profile 只能解析 OAuth 或静态凭证。请把该 profile 放在 Aliyun-compatible 配置文件中。

有一个值得点名的陷阱：ecctl 原生 profile 声明了 `mode: RamRoleArn`，但同时携带 `access_key_id` 和 `access_key_secret` 时，会直接使用这对 AccessKey，并忽略 `ram_role_arn`。不会扮演任何角色。请把该 profile 放在 Aliyun-compatible 文件中。

## 使用环境变量配置

```bash
export ALIBABA_CLOUD_ACCESS_KEY_ID=<source-id>
export ALIBABA_CLOUD_ACCESS_KEY_SECRET=<source-secret>
export ALIBABA_CLOUD_ROLE_ARN=acs:ram::1234567890123456:role/demo
export ALIBABA_CLOUD_ROLE_SESSION_NAME=ecctl-session
```

在环境变量链中，AccessKey 密钥对加上 `ALIBABA_CLOUD_ROLE_ARN` 会成为 `RamRoleArn`。没有角色 ARN 时，同一对密钥仍是 [`AK`](./ak.md)。该路径还会读取 `ALIBABA_CLOUD_EXTERNAL_ID`、`ALIBABA_CLOUD_STS_ENDPOINT` 和 `ALIBABA_CLOUD_STS_REGION`。

## profile 字段

| 字段 | 必填 | 说明 |
|---|---|---|
| `mode` | 否 | `RamRoleArn`。当 `ram_role_arn` 与 AccessKey 密钥对同时存在时推导得出 |
| `access_key_id` | 是 | 源 AccessKey ID。fallback 到环境变量 |
| `access_key_secret` | 是 | 源 AccessKey secret。fallback 到环境变量 |
| `sts_token` | 否 | 源本身是临时凭证时存在 |
| `ram_role_arn` | 是 | 完整 ARN：`acs:ram::<16位账号ID>:role/<角色名>` |
| `ram_session_name` | 否 | 角色会话名称。fallback 到 `ALIBABA_CLOUD_ROLE_SESSION_NAME` |
| `expired_seconds` | 否 | 请求的会话时长，单位为秒 |
| `policy` | 否 | 进一步收窄所扮演会话的内联 policy |
| `external_id` | 否 | 角色信任 policy 要求的 External ID |
| `sts_endpoint` | 否 | 自定义 STS 端点。必须为 HTTPS |
| `sts_region` | 否 | 选择地域 STS 端点 |
| `enable_vpc` | 否 | 使用 VPC STS 端点 |

```json
{
  "name": "cross-account",
  "mode": "RamRoleArn",
  "access_key_id": "LTAI5t...",
  "access_key_secret": "...",
  "ram_role_arn": "acs:ram::1234567890123456:role/demo",
  "ram_session_name": "ecctl-session",
  "expired_seconds": 3600,
  "region_id": "cn-hangzhou"
}
```

`ram_session_name`、`expired_seconds` 和 `policy` 按给定值原样传递。省略时 `ecctl` 不发送任何值，由阿里云凭证 SDK 和 STS 应用各自的默认值。

## ARN 校验与身份验证

`ram_role_arn` 必须是完整的 `acs:ram::<16位账号ID>:role/<角色名>` ARN。`ecctl` 从该 ARN 派生预期账号，然后在第一条业务请求之前通过官方 STS `GetCallerIdentity` 端点验证初始凭证。账号段缺失或格式错误的 ARN 会在本地失败，而不是产生令人困惑的服务错误。

显式自定义的 `sts_endpoint` 可以签发凭证，但永远不会被信任去验证它自己签发的结果。独立的身份检查仍然走官方端点。自定义端点必须是 HTTPS 主机，不能带有用户信息、路径、查询或片段：

```json
{
  "error": {
    "kind": "client",
    "code": "InvalidCredentials",
    "message": "STS endpoint must be an HTTPS host without user information, path, query, or fragment"
  }
}
```

当身份检查必须使用地域或 VPC STS 端点时，设置 `sts_region` 和 `enable_vpc`。

## 验证

```bash
ecctl --profile cross-account configure get
ecctl --profile cross-account --region cn-hangzhou ecs region list
```

被 STS 拒绝的源凭证会产生 `refresh session token failed` 以及 STS 响应体，其中点名 `sts.aliyuncs.com` 或所配置的地域端点，并携带 Request ID。这说明解析已经走到了扮演角色这一步。

## 相关文档

- [链式 RAM 角色 ARN](./chainable-ram-role-arn.md)：通过命名的源 profile 扮演角色
- [OIDC](./oidc.md)：无需存储源密钥的工作负载身份
- [凭证总览](./index.md)

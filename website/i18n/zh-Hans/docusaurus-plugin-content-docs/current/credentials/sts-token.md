---
title: STS 令牌
description: 在 ecctl 中使用已有的临时 AccessKey、secret 和 security token。
---

# STS 令牌

`StsToken` 携带由其他地方签发的临时凭证：一个 AccessKey ID、一个 AccessKey secret 和一个 security token。`ecctl` 完全按给定的值使用它们，无法刷新。凭证过期后命令会失败，必须由 `ecctl` 之外的组件签发新的一组凭证。

当另一个系统已经在发放短期凭证时使用该模式，例如自己调用 STS `AssumeRole` 的 CI 作业。需要由 `ecctl` 来扮演角色时，改用 [RAM 角色 ARN](./ram-role-arn.md)。需要从端点获取并续期凭证时，使用 [Credentials URI](./credentials-uri.md)。

## 使用 ecctl 配置

```bash
ecctl configure set access-key-id <temporary-id>
ecctl configure set access-key-secret <temporary-secret>
ecctl configure set security-token <token>
```

`security-token` 存储为 `sts_token`。在已经持有 AccessKey 密钥对的 profile 上设置它，会把该 profile 从 `AK` 切换为 `StsToken`。

不支持 `ecctl configure --mode StsToken`。`--mode` 只接受 `OAuth`。

## 使用阿里云 CLI 配置

```bash
aliyun configure --mode StsToken --profile ci
```

## 使用环境变量配置

```bash
export ALIBABA_CLOUD_ACCESS_KEY_ID=<temporary-id>
export ALIBABA_CLOUD_ACCESS_KEY_SECRET=<temporary-secret>
export ALIBABA_CLOUD_SECURITY_TOKEN=<token>
```

三者都是必需的。只有 AccessKey 密钥对而没有 security token 时解析为 [`AK`](./ak.md)，而不是一个不完整的 `StsToken`。每个变量也接受 `ALIBABACLOUD_`、`ALICLOUD_` 前缀或裸写形式。

## profile 字段

| 字段 | 必填 | 说明 |
|---|---|---|
| `mode` | 否 | `StsToken`。当 `sts_token` 与 AccessKey 密钥对同时存在时推导得出 |
| `access_key_id` | 是 | 临时 AccessKey ID，通常以 `STS.` 前缀开头 |
| `access_key_secret` | 是 | 临时 AccessKey secret |
| `sts_token` | 是 | security token |
| `sts_expiration` | 否 | 以秒为单位的 Unix 时间戳。启用过期检查 |

```json
{
  "name": "ci",
  "mode": "StsToken",
  "access_key_id": "STS.NUgYrLnoC...",
  "access_key_secret": "...",
  "sts_token": "...",
  "sts_expiration": 1782460800,
  "region_id": "cn-hangzhou"
}
```

`sts_expiration` 是以秒为单位的 Unix 时间戳整数，而不是 RFC 3339 字符串。

## 过期行为

没有 `sts_expiration` 时，`ecctl` 直接发送该凭证，由服务判定。已过期的令牌随后在 API 侧以服务错误失败。

有 `sts_expiration` 时，`ecctl` 会在签名前拒绝已过期的令牌，也会拒绝无法覆盖已知命令 deadline 的令牌。这把一次传输中途的失败变成立刻出现的本地错误，在长时间操作中更容易定位。

因为 `StsToken` 无法自行刷新，长时间 OSS 传输需要一个远超出传输窗口的 `sts_expiration`。无法保证这一点时，改用可续期模式：[OAuth](./oauth.md)、[OIDC](./oidc.md)、[RAM 角色 ARN](./ram-role-arn.md)、[ECS RAM 角色](./ecs-ram-role.md)、[External](./external.md) 或 [Credentials URI](./credentials-uri.md)。

## 验证

```bash
ecctl --profile ci configure get
ecctl --profile ci --region cn-hangzhou ecs region list
```

格式错误的 security token 由服务端拒绝，因此表现为 `CloudAPIError` 服务错误，并带上服务响应中的名称，例如 `Specified SecurityToken is malformed`。该响应携带 Request ID，说明凭证已在本地完成解析和签名，是服务本身拒绝了这个令牌。如果没有 Request ID，而是客户端 `InvalidCredentials` 或 `MissingCredentials` 错误，则说明凭证根本没有解析出来。

## 相关文档

- [AccessKey](./ak.md)：长期有效的密钥
- [Credentials URI](./credentials-uri.md)：通过 HTTP 获取可续期 STS 凭证
- [身份凭证](./index.md)：解析顺序

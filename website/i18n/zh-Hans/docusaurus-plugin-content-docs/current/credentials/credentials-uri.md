---
title: Credentials URI
description: 从 HTTPS 或 loopback HTTP 端点获取可续期的 STS 凭证。
---

# Credentials URI

`CredentialsURI` 从一个 HTTP 端点获取临时凭证，而不是运行本地程序。当凭证 broker 可以通过网络访问、由 sidecar 在 localhost 上分发凭证，或者容器平台把凭证 URL 注入到环境中时，使用它。

该凭证是可续期的，因此随着它接近过期，`ecctl` 会在后续已签名请求之前重新获取。

## 使用阿里云 CLI 配置

```bash
aliyun configure --mode CredentialsURI --profile broker
```

`ecctl configure --mode CredentialsURI` 不受支持。`--mode` 只接受 `OAuth`，而 ecctl 原生 profile 只能解析 OAuth 或静态凭证。ecctl 配置文件中声明 `CredentialsURI` 却没有静态凭证的 profile 会以 `MissingCredentials` 失败。请把该 profile 放在兼容 `aliyun` 的配置文件中。

## 使用环境变量配置

```bash
export ALIBABA_CLOUD_CREDENTIALS_URI=https://broker.internal/credentials
```

有两条路径会走到这个来源。在环境变量链中，只有在没有选中任何已存储 profile，或者 `ALIBABA_CLOUD_IGNORE_PROFILE=TRUE` 强制走纯环境变量路径时才会查询它；它在一组 access key、一组完整的 OIDC 配置和 `ALIBABA_CLOUD_ECS_METADATA` 之后测试，在 `ALIBABA_CLOUD_BEARER_TOKEN` 之前测试。第二条路径是匹配到的 profile 声明了 `CredentialsURI` 却把 `credentials_uri` 留空，此时会回退到 `ALIBABA_CLOUD_CREDENTIALS_URI`。两者都没有 URI 时，命令以 `InvalidCredentials` 失败，而不会退化成碰巧导出的 AccessKey 密钥对。

## profile 字段

| 字段 | 必填 | 说明 |
|---|---|---|
| `mode` | 否 | `CredentialsURI`。存在 `credentials_uri` 时可推断 |
| `credentials_uri` | 是 | 为空时回退到 `ALIBABA_CLOUD_CREDENTIALS_URI` |

```json
{
  "name": "broker",
  "mode": "CredentialsURI",
  "credentials_uri": "https://broker.internal/credentials?role=ecctl",
  "region_id": "cn-hangzhou"
}
```

## 传输要求

必须使用 HTTPS，只有一个例外：host 是字面量 loopback IP 地址的 URL 可以使用 HTTP。

```json
{
  "error": {
    "kind": "client",
    "code": "InvalidCredentials",
    "message": "credentials URI requires HTTPS unless it uses a literal loopback address"
  }
}
```

`http://127.0.0.1:8080/credentials` 和 `http://[::1]:8080/credentials` 会被接受。`http://localhost:8080/credentials` 不会，因为 `localhost` 是一个主机名，DNS 可以把它指向任何地方。这样一来 localhost sidecar 可用，同时任何离开本机的请求都不会用明文传输凭证。

请求是一个普通的 `GET`，超时 15 秒，响应体最多读取 1 MiB，超出部分丢弃。重定向不会被跟随：`3xx` 会原样返回并在状态码检查处失败，因此藏在重定向后面的 broker 会报 `returned HTTP 302`，请求也永远不会发给第二个主机。请把 `credentials_uri` 直接指向最终 URL。

## 响应契约

端点必须返回 HTTP 200，并带上一个 JSON 响应体：

| 字段 | 必填 | 说明 |
|---|---|---|
| `Code` | 是 | 必须恰好是 `Success` |
| `AccessKeyId` | 是 | |
| `AccessKeySecret` | 是 | |
| `SecurityToken` | 是 | 始终必填，与 External 辅助程序契约不同 |
| `Expiration` | 是 | RFC 3339，必须带明确的时区偏移，且是未来时间 |

```json
{
  "Code": "Success",
  "AccessKeyId": "STS.NUgYrLnoC...",
  "AccessKeySecret": "...",
  "SecurityToken": "...",
  "Expiration": "2026-09-03T12:00:00Z"
}
```

注意字段命名：该契约使用 PascalCase，而 [External](./external.md) 辅助程序契约使用 snake_case。

失败情况及其消息如下，其中 `<source>` 是去掉 path 后的 URI：

| 情况 | 消息 |
|---|---|
| 非 200 状态码 | `credential source <source> returned HTTP <code>` |
| `Code` 不是 `Success` | `credential source <source> returned incomplete credentials` |
| `AccessKeyId`、`AccessKeySecret`、`SecurityToken` 中任意一个为空 | `credential source <source> returned incomplete credentials` |
| `Expiration` 缺失或无法解析 | `credential source <source> returned an invalid expiration` |
| `Expiration` 不是未来时间 | `credential source <source> returned expired credentials` |

非 `Success` 的 `Code` 和缺失字段会产生完全相同的消息，因此返回结构化失败响应体的端点看起来就像响应不完整。看到该消息时请检查端点自己的日志。

`Expiration` 在这里是必填的。没有有效未来过期时间的 CredentialsURI 响应总是会被拒绝，正因如此 `ecctl` 才能把该凭证当作可续期凭证并自行安排刷新。

时区偏移必须写，但不必是 UTC。`2026-09-03T12:00:00Z` 和 `2026-09-03T20:00:00+08:00` 都能解析，`2026-09-03T12:00:00` 不带偏移，会被拒绝并报 `returned an invalid expiration`。

## 禁用该来源

```bash
export ALIBABA_CLOUD_DISABLE_EXTERNAL_PROCESS=true
```

```json
{
  "error": {
    "kind": "client",
    "code": "CredentialSourceDisabled",
    "message": "ALIBABA_CLOUD_DISABLE_EXTERNAL_PROCESS disables CredentialsURI credentials"
  }
}
```

该变量不区分大小写，接受 `1` 或 `true`，并且同时会禁用 [External](./external.md)。在配置文件或环境可能被他人影响的地方设置它，注入的 URL 就无法被访问。

## 验证

```bash
ecctl --profile broker configure get
ecctl --profile broker --region cn-hangzhou ecs region list
```

先独立检查端点。URL 要加引号：未加引号的 `?` 在 `bash` 和 `zsh` 中是通配符，命令会在 `curl` 运行之前就中止。

```bash
curl -s 'https://broker.internal/credentials?role=ecctl'
```

响应体是一份有效凭证。它会把可用的 access key secret 和 security token 打印到终端上，留在滚屏历史里，也留在任何会话录制里。旁边有人能看到屏幕时，只确认结构而不看具体值：

```bash
curl -s 'https://broker.internal/credentials?role=ecctl' | jq 'keys'
```

确认响应体带有 `Code: "Success"`、全部四个凭证字段，以及一个足够靠后的 `Expiration`，能够覆盖你即将执行的操作。

## 续期与身份固定

随着凭证接近过期，`ecctl` 会在后续已签名请求之前重新获取，因此长时间操作可以持续运行。首个凭证会固定规范化身份；后续获取如果返回不同身份，会在它能够签名请求之前被拒绝。

## 相关文档

- [External 进程](./external.md)：对应的本地程序方式
- [STS 令牌](./sts-token.md)：固定的临时凭证
- [身份凭证](./index.md)：解析顺序

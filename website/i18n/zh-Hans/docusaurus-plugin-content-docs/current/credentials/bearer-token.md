---
title: Bearer 令牌
description: 用 ecctl 鉴权那些接受 bearer 令牌的产品 API。
---

# Bearer 令牌

`BearerToken` 在请求头中发送一个令牌，而不是用 access key 为请求签名。它只适用
于接受 bearer 鉴权的产品 API。它不是通用凭证，也不是绕过缺失 access key 的办法。

令牌是静态的。`ecctl` 无法续期它，也没有过期时间字段可供检查。

## 使用阿里云 CLI 配置

```bash
aliyun configure --mode BearerToken --profile bearer
```

`ecctl configure --mode BearerToken` 不受支持。`--mode` 只接受 `OAuth`，而 ecctl
原生 profile 只能解析 OAuth 或静态凭证。请把该 profile 放在 Aliyun-compatible
配置文件中。

## 使用环境变量配置

```bash
export ALIBABA_CLOUD_BEARER_TOKEN=<token>
export ALIBABA_CLOUD_BEARER_TOKEN_HEADER_KEY=<header-name>
```

header 变量是可选的。在环境变量链中，这是最后查询的来源，排在一组 access key、
一组完整的 OIDC 配置、`ALIBABA_CLOUD_ECS_METADATA` 和
`ALIBABA_CLOUD_CREDENTIALS_URI` 之后。任何更早解析成功的来源都会胜出。

## profile 字段

| 字段 | 必填 | 说明 |
|---|---|---|
| `mode` | 否 | `BearerToken`。存在 `bearer_token` 时可推断 |
| `bearer_token` | 是 | 为空时回退到 `ALIBABA_CLOUD_BEARER_TOKEN` |
| `bearer_token_header_key` | 否 | 默认为 `x-acs-bearer-token` |

```json
{
  "name": "bearer",
  "mode": "BearerToken",
  "bearer_token": "...",
  "bearer_token_header_key": "x-acs-bearer-token",
  "region_id": "cn-hangzhou"
}
```

只有当目标 API 的文档写明了不同的 header 名称时，才设置
`bearer_token_header_key`。默认值就是大多数 bearer 鉴权产品 API 所期望的。

## 适用范围限制

bearer 令牌只会被支持 bearer 鉴权的 API 接受。不支持它的 API 会在服务端拒绝
请求：

```json
{
  "error": {
    "kind": "service",
    "code": "CloudAPIError",
    "message": "API call failed; see actions for details"
  },
  "actions": [
    {
      "action_name": "DescribeRegions",
      "code": "403, This signature type is not supported.",
      "message": "code: 403, This signature type is not supported."
    }
  ]
}
```

这个响应是该模式按设计正常工作的最清晰信号。令牌已被解析、附加到请求上并送达；
只是目标 API 不接受 bearer 鉴权。注意 `kind` 是 `service` 而不是 `client`：
`ecctl` 并没有在生成凭证这一步失败。

`This signature type is not supported` 不是需要在 profile 中修复的配置问题。它的
含义是你调用的操作并不是 bearer 鉴权操作。普通资源命令请使用
[OAuth](./oauth.md)、[AK](./ak.md) 或 [RAM 角色 ARN](./ram-role-arn.md) 这类
签名模式，把 `BearerToken` 留给明确要求它的 API。

## 验证

```bash
ecctl --profile bearer configure get
```

只在确实需要时才把令牌读回来：

```bash
ecctl configure get bearer_token --show-secret
```

由于没有本地有效性检查，唯一真正的验证方式是向一个接受 bearer 鉴权的 API 发起
请求。来自无关 API 的 403 无法说明令牌是否有效。

## 相关文档

- [STS token](./sts-token.md)：临时的已签名凭证
- [凭证总览](./index.md)：模式选择

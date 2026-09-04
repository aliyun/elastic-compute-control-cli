---
title: CloudSSO
description: 在 ecctl 中使用阿里云 CloudSSO 访问配置。
---

# CloudSSO

`CloudSSO` 使用企业的 CloudSSO 访问配置，为成员账号获取临时凭证。用户只需通过组织的身份 provider 登录一次，随后 `ecctl` 把得到的 access token 换取为限定在所选账号和访问配置范围内的 STS 凭证。

当你的组织通过 CloudSSO 而不是通过单独的 RAM 用户或长期 AccessKey 来管理阿里云访问时，请使用该凭证模式。

`ecctl` 会消费 CloudSSO profile，但不会执行交互式的 CloudSSO 登录。请先用阿里云 CLI 配置该 profile，再从 `ecctl` 使用它。

## 使用阿里云 CLI 配置

```bash
aliyun configure --mode CloudSSO --profile sso
```

交互式流程会打开浏览器，让你选择成员账号和访问配置，并写入该 profile。不支持 `ecctl configure --mode CloudSSO`；`--mode` 只接受 `OAuth`。

需要由 `ecctl` 自身管理的原生浏览器登录时，请使用 [OAuth](./oauth.md)。

## profile 字段

| 字段 | 必填 | 说明 |
|---|---|---|
| `mode` | 否 | `CloudSSO`。存在 `cloud_sso_sign_in_url` 时会被推断 |
| `cloud_sso_sign_in_url` | 是 | 组织的 CloudSSO 门户 URL |
| `cloud_sso_account_id` | 是 | 该凭证指向的成员账号 |
| `cloud_sso_access_config` | 是 | 访问配置的名称或 ID |
| `access_token` | 否 | 缓存的 CloudSSO 访问令牌 |
| `cloud_sso_access_token_expire` | 否 | 缓存令牌的 Unix 时间戳（秒） |

```json
{
  "name": "sso",
  "mode": "CloudSSO",
  "cloud_sso_sign_in_url": "https://signin-cn-hangzhou.alibabacloudsso.com/login/....htm",
  "cloud_sso_account_id": "1234567890123456",
  "cloud_sso_access_config": "AdministratorAccess",
  "region_id": "cn-hangzhou"
}
```

`cloud_sso_account_id` 和 `cloud_sso_access_config` 都是必需的。缺少其中任意一项的 profile 会在解析过程中失败，而不会回退到一个未限定范围的凭证。

`access_token` 和 `cloud_sso_access_token_expire` 是登录流程写入的缓存字段。缓存令牌缺失或其过期时间已过时，`ecctl` 会重新发起一次登录。

## 可续期状态的存放位置

云命令把兼容 `aliyun` 的配置文件视为只读。轮换后的 CloudSSO 令牌和缓存的 CloudSSO STS 凭证只持久化在 `~/.ecctl/credentials-v2/` 下规范化的、按用户隔离的 ecctl 凭证存储中，权限仅限当前用户。条目按解析到的兼容 `aliyun` 的配置文件路径和 profile 分别作为键，因此两个不同的配置文件即使持有同名 profile，也不会共享条目。

`ECCTL_CONFIG_PATH` 不会为 CloudSSO profile 创建第二个轮换所有者，私有存储也不会随 `ECCTL_CONFIG_PATH` 移动。

## 身份固定

`cloud_sso_account_id` 被视为该 profile 的预期账号。属于其他账号的刷新凭证会在其签名请求前被拒绝，因此 CloudSSO 分配发生变化、或重新认证进入了另一个成员账号时会失败退出，而不会在命令执行途中切换账号。

身份固定不只作用于 CloudSSO。所有可续期凭证在一条命令的执行期间都会被固定，刷新后身份发生变化的凭证会让命令失败，而不是顶着新身份继续执行。

唯一不会中断命令的问题是缓存写入失败。CloudSSO 刷新成功、但新凭证无法写入私有存储时，`ecctl` 会用内存中的凭证把这条命令跑完，下一次命令再重新登录。其他任何持久化失败都是致命的。

## 验证

```bash
ecctl --profile sso configure get
ecctl --profile sso --region cn-hangzhou ecs region list
```

`configure get` 报告所声明的 profile，不发起云端调用。第二条命令走的是真实路径：令牌有效性、账号与访问配置的范围限定，以及 STS 换取。

缓存令牌已过期且没有可用的浏览器会话时，命令会失败，而不会静默使用过期凭证。请重新执行 `aliyun configure --mode CloudSSO --profile sso`。

## 相关文档

- [OAuth](./oauth.md)：由 `ecctl` 原生管理的浏览器登录
- [身份凭证](./index.md)：解析顺序与私有存储

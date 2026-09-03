---
title: OAuth
description: 使用基于浏览器的阿里云 OAuth 登录为 ecctl 完成认证。
---

# OAuth

`OAuth` 是推荐给在终端前操作的人员使用的模式。它用一次浏览器登录换取 refresh token，然后按需用它换取短期 STS 凭证。ecctl 配置文件中不会在磁盘上保存任何长期有效的密钥，并且 `ecctl` 中唯一带有原生交互式配置流程的凭证模式就是 `OAuth`。

`OAuth` 是可续期的，因此适合长时间运行的操作，例如大体积 OSS 传输。

## 登录

```bash
ecctl configure --mode OAuth --profile production
```

该命令会打开浏览器，等待授权回调，验证账号，然后写入 profile。成功时 stdout 只包含 profile、模式、站点、经过验证的账号 ID、配置路径和浏览器启动状态，绝不包含任何令牌。

直接使用结果：

```bash
ecctl --profile production ecs instance list --region cn-hangzhou
```

## 站点类型

默认站点为 `CN`。账号属于国际站时，请选择国际站：

```bash
ecctl configure --mode OAuth --profile production --oauth-site-type INTL
```

| `--oauth-site-type` | OAuth 端点 | 登录端点 |
|---|---|---|
| `CN` | `https://oauth.aliyun.com` | `https://signin.aliyun.com` |
| `INTL` | `https://oauth.alibabacloud.com` | `https://signin.alibabacloud.com` |

其他任何取值都会以 `OAuth site type must be CN or INTL` 失败。

## 账号绑定

一次登录绑定到一个阿里云账号。首次交互式登录会显示经过验证的账号 ID，并要求你在保存凭证之前输入完整的 ID。后续登录必须与该 profile 已记录的账号一致。

在非交互终端中，或者已经提前知道目标账号时，请显式传入：

```bash
ecctl configure --mode OAuth --profile production --expected-account-id 1234567890123456
```

传入不同的 `--expected-account-id` 是授权有意切换账号的显式方式。没有它，针对其他账号的登录会被拒绝，而不会静默替换已记录的身份。

## 手工打开浏览器

登录使用 PKCE，HTTP 回调只绑定到 `127.0.0.1` 的 12345 到 12349 端口。自动打开浏览器成功时，不会输出一次性授权 URL。

必须手工打开该 URL 时，请在私有终端中使用 `--manual`：

```bash
ecctl configure --mode OAuth --profile production --manual
```

不要把一次性 URL 复制到共享日志、issue 跟踪系统或聊天中。它是待处理授权的持有者。浏览器启动进程不会继承阿里云或 OSS 凭证环境变量。

## 自定义元数据路径

默认位置不合适时，请显式选择 ecctl 元数据文件：

```bash
ecctl configure --mode OAuth --profile production --config-path /path/to/config.json
```

后续资源命令需要使用同一个文件时，请导出 `ECCTL_CONFIG_PATH=/path/to/config.json` 并选择同名 profile。原生 OAuth 的配置路径不能与 aliyun CLI 配置路径相同。

私有凭证存储不随 `ECCTL_CONFIG_PATH` 改变。它始终位于按当前进程 home 目录解析出的 `~/.ecctl/credentials-v2/` 下。

## 存储位置

| 位置 | 内容 |
|---|---|
| `~/.ecctl/config.json` | 非敏感元数据：`mode`、`oauth_site_type`、`oauth_generation`、`oauth_account_id`，以及地域、语言和输出 |
| `~/.ecctl/credentials-v2/` | access token、refresh token 和换取到的 STS 凭证，权限为仅当前用户可访问 |

原生 OAuth 条目每个 profile 只有一个 per-user owner。login generation 发生变化时会让较旧的元数据失效，而不是切换身份。令牌绝不会写入兼容 `aliyun` 的配置文件。

原生 OAuth cache 的每次写入都会在 per-profile 锁内比较 active generation。登录还会保留一个私有 write-ahead transaction，直到 cache 与 ecctl metadata 的 generation 一致。如果进程或主机在这两次写入之间停止，下一次登录或凭证加载会先恢复旧 generation 或完成新 generation，再使用任何令牌。如果服务端 refresh token 轮换无法提交到本地，`ecctl` 会停止且不重试该轮换，该 profile 必须重新认证。

## 验证

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

## 重新认证

原生 OAuth 认证过期时，请重新执行登录：

```bash
ecctl configure --mode OAuth --profile production
```

来源于兼容 `aliyun` 配置的 OAuth profile 使用 `aliyun configure` 重新认证，而不是用 `ecctl`。

## 使用已有的 OAuth profile

`ecctl` 也会从兼容 `aliyun` 的配置文件中读取 OAuth profile。这类 profile 带有 `oauth_site_type`、`oauth_refresh_token`、`oauth_refresh_token_expire`、`oauth_access_token` 和 `oauth_access_token_expire`。这些 profile 轮换后的令牌和缓存的 STS 凭证会按 profile 分别存为 `~/.ecctl/credentials-v2/` 下的条目，并按解析到的来源路径和 profile 建立键。兼容 `aliyun` 的配置文件本身在云命令执行期间被视为只读。

ecctl 原生 OAuth 元数据优先于同名的兼容 `aliyun` 的 profile。

## 相关文档

- [身份凭证](./index.md)：解析顺序
- [CloudSSO](./cloudsso.md)：由浏览器支撑的企业单点登录
- [配置](../getting-started/configuration.md)：地域、语言、输出和 profile 管理

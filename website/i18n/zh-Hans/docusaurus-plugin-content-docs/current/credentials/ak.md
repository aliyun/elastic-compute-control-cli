---
title: AccessKey
description: 为 ecctl 配置长期有效的阿里云 AccessKey 密钥对。
---

# AccessKey

`AK` 使用长期有效的 AccessKey ID 和 AccessKey secret。它是最简单的凭证模式，也最容易被误用：secret 不会过期，拥有其 RAM policy 允许的全部权限，并且落在你存放它的磁盘上。

交互式工作请优先使用 [OAuth](./oauth.md)，自动化请使用 [OIDC](./oidc.md)、[ECS RAM 角色](./ecs-ram-role.md)或 [RAM 角色 ARN](./ram-role-arn.md) 这类可续期模式。只有在确实无法使用可续期模式时才使用 `AK`。

`AK` 不可续期。对于长时间 OSS 传输或长时间运行的脚本，静态密钥没有问题，因为它不会在操作中途过期，但它也永远不会自动轮换。

## 使用 ecctl 配置

```bash
ecctl configure set access-key-id <id>
ecctl configure set access-key-secret <secret>
```

每条命令都会写入 `~/.ecctl/config.json` 中所选的 profile，并回显存储的键：

```json
{
  "key": "access-key-secret",
  "profile": "default",
  "sensitive": true,
  "value": "********"
}
```

只设置这两个字段会得到一个 `AK` profile。之后再添加 `security-token`，同一个 profile 就会变成 [`StsToken`](./sts-token.md)。

用 `--profile` 写入命名 profile：

```bash
ecctl --profile production configure set access-key-id <id>
ecctl --profile production configure set access-key-secret <secret>
```

不支持 `ecctl configure --mode AK`。`--mode` 只接受 `OAuth`；`AK` 模式由是否存在 AccessKey 密钥对推导得出。

## 使用阿里云 CLI 配置

`ecctl` 会把兼容 `aliyun` 的配置文件作为只读的回退来源读取，因此已有的 profile 仍可继续使用：

```bash
aliyun configure --mode AK --profile production
```

## 使用环境变量配置

```bash
export ALIBABA_CLOUD_ACCESS_KEY_ID=<id>
export ALIBABA_CLOUD_ACCESS_KEY_SECRET=<secret>
```

以下情况会读取环境变量凭证：未选中任何已存储的 profile、选中的 ecctl profile 不携带凭证，或者 `ALIBABA_CLOUD_IGNORE_PROFILE=TRUE` 强制走纯环境变量路径。只设置两个变量中的一个是硬错误，而不是得到一个不完整的凭证：

```json
{
  "error": {
    "kind": "client",
    "code": "InvalidCredentials",
    "message": "both ALIBABA_CLOUD_ACCESS_KEY_ID and ALIBABA_CLOUD_ACCESS_KEY_SECRET are required"
  }
}
```

每个变量也接受 `ALIBABACLOUD_`、`ALICLOUD_` 前缀，或裸写的 `ACCESS_KEY_ID` / `ACCESS_KEY_SECRET`。

## profile 字段

| 字段 | 必填 | 说明 |
|---|---|---|
| `mode` | 否 | `AK`。缺失时由 `access_key_id` 和 `access_key_secret` 推导 |
| `access_key_id` | 是 | 为空时回退到环境变量 |
| `access_key_secret` | 是 | 敏感。为空时回退到环境变量 |

兼容 `aliyun` 的配置文件中省略 `mode` 的 profile 会按其字段分类。携带 AccessKey 密钥对、且没有 `sts_token` 或 `ram_role_arn` 的 profile 解析为 `AK`。

```json
{
  "name": "production",
  "mode": "AK",
  "access_key_id": "LTAI5t...",
  "access_key_secret": "...",
  "region_id": "cn-hangzhou"
}
```

## 验证

```bash
ecctl --profile production configure get
ecctl --profile production --region cn-hangzhou ecs region list
```

`configure get` 不发起云端调用即报告声明的模式。第二条命令证明这把密钥能够签名真实请求。未知或已禁用的密钥返回 `404, Specified access key is not found`；该响应携带 Request ID，说明解析和签名都已成功，是服务本身拒绝了这把密钥。

## 存储与权限

新建的 ecctl 配置文件权限为 `0600`。对已存在文件的写入则保留该文件原有的权限，因此一个 `0644` 的配置在之后每次 `configure set` 之后仍然是 `0644`。请自行确认，如果过于宽松就收紧：

```bash
ls -l ~/.ecctl/config.json
chmod 600 ~/.ecctl/config.json
```

`~/.ecctl/credentials-v2/` 下的私有凭证存储保存轮换后的 OAuth 与 CloudSSO 令牌以及缓存的 STS 凭证，它始终以规范化的仅当前用户可访问权限写入。

只在确实需要时才读回存储的 secret：

```bash
ecctl configure get access-key-secret --show-secret
```

在 `configure get`、`configure list` 以及所有其他输出路径中，敏感值默认掩码为 `********`。

## 相关文档

- [STS 令牌](./sts-token.md)：临时 AccessKey 凭证
- [身份凭证](./index.md)：解析顺序
- [配置](../getting-started/configuration.md#支持的配置项)：可设置的配置项列表

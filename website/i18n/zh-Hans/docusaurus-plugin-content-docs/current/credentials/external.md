---
title: External 进程
description: 运行本地凭证辅助程序，为 ecctl 提供凭证。
---

# External 进程

`External` 运行一个本地程序，并从它的 stdout 读取凭证。用它可以把 `ecctl`
桥接到已有的密钥管理服务、企业凭证 broker 或 vault agent，而不需要在配置文件中
保存任何内容。

辅助程序由解析后的 argv 直接执行，绝不会交给 shell 求值，因此
`process_command` 中的 shell 元字符不会被解释。

## 使用阿里云 CLI 配置

```bash
aliyun configure --mode External --profile vault
```

`ecctl configure --mode External` 不受支持。`--mode` 只接受 `OAuth`，而 ecctl
原生 profile 只能解析 OAuth 或静态凭证。ecctl 配置文件中声明 `External` 却没有
静态凭证的 profile 会以 `MissingCredentials` 失败。请把该 profile 放在
Aliyun-compatible 配置文件中。

## profile 字段

| 字段 | 必填 | 说明 |
|---|---|---|
| `mode` | 否 | `External`。存在 `process_command` 时可推断 |
| `process_command` | 是 | 命令行，解析为 argv |

```json
{
  "name": "vault",
  "mode": "External",
  "process_command": "/usr/local/bin/vault-aliyun-credential --role ecctl",
  "region_id": "cn-hangzhou"
}
```

profile 完全没有 `process_command` 时以 `process_command is required for External credentials` 失败。另一条消息 `process_command is empty` 表示该字段存在但分词后得到零个参数，出现在值全部由引号或空白组成的情况下。

### 引号与分词

命令按引号感知的方式分词。单引号和双引号会把参数组合在一起，因此包含空格的
路径也能正常工作：

```json
{"process_command": "\"/opt/my tools/get-credential\" --profile 'team a'"}
```

Windows profile 按 Windows 规则分词。由于不经过任何 shell，
`process_command` 中的环境变量展开、管道、重定向和命令替换都不会发生。请在
辅助程序内部自行处理这些需求。

## 输出契约

辅助程序必须向 stdout 打印一个 JSON 对象：

| 字段 | 必填 | 说明 |
|---|---|---|
| `mode` | 是 | 只能是 `AK` 或 `StsToken` |
| `access_key_id` | 是 | |
| `access_key_secret` | 是 | |
| `sts_token` | `StsToken` 时必填 | Security token |
| `expiration` | 否 | RFC 3339 UTC。存在时必须是未来时间 |

```json
{
  "mode": "StsToken",
  "access_key_id": "STS.NUgYrLnoC...",
  "access_key_secret": "...",
  "sts_token": "...",
  "expiration": "2026-09-03T12:00:00Z"
}
```

其他任何 `mode` 都会被拒绝：

```json
{
  "error": {
    "kind": "client",
    "code": "InvalidCredentials",
    "message": "external credential command returned an unsupported mode"
  }
}
```

这意味着 External 辅助程序本身无法返回 OAuth、OIDC 或角色凭证。它返回一对可用
的密钥，可以是临时的。

缺失必填字段会产生 `external credential command returned incomplete credentials`。`expiration` 格式错误、缺失或已过期，会产生 `external credential command returned an invalid expiration` 或 `external credential command returned expired credentials`。非法 JSON 会产生 `external credential command returned invalid JSON`。

`mode: AK` 允许省略 `expiration`；此时密钥会作为静态凭证传给本次操作，在
`ecctl` 看来永不过期。

## 执行限制

| 限制 | 取值 |
|---|---|
| 获取 deadline | 60 秒 |
| 捕获的 stdout | 1 MiB |
| 取消后宽限 | 2 秒 |

输出超过 1 MiB 会以 `external credential output exceeds size limit` 失败。
超过 deadline 的辅助程序按获取失败处理。

辅助程序内部的任何失败都会报告为 `external credential command failed`。辅助程序自己的 stderr 和退出码不会回显到 `ecctl` 错误中，因此需要诊断失败时请在辅助程序侧记录它们。

在 Unix 上，取消时会终止辅助程序的整个进程组。在所有平台上，两秒宽限期结束后
都会强制释放继承的输出管道，因此派生了长生命周期子进程的辅助程序无法让命令一直
挂着。

## 禁用该来源

由于 `External` 会执行本地程序，它可以被整体关闭：

```bash
export ALIBABA_CLOUD_DISABLE_EXTERNAL_PROCESS=true
```

```json
{
  "error": {
    "kind": "client",
    "code": "CredentialSourceDisabled",
    "message": "ALIBABA_CLOUD_DISABLE_EXTERNAL_PROCESS disables External credentials"
  }
}
```

该变量不区分大小写，接受 `1` 或 `true`。它同时会禁用 `CredentialsURI`。在配置
文件可能被他人影响的环境中设置它，注入的 `process_command` 就无法运行。

## 验证

```bash
ecctl --profile vault configure get
ecctl --profile vault --region cn-hangzhou ecs region list
```

先直接运行辅助程序，确认它的 stdout 恰好是一个 JSON 对象，前面没有任何日志
前导输出：

```bash
/usr/local/bin/vault-aliyun-credential --role ecctl
```

JSON 之前打印的任何内容都会让输出无法解析。请把诊断信息写到 stderr。

## 续期

凭证是否续期取决于 `expiration`。存在未来的 `expiration` 时，随着凭证接近过期，
`ecctl` 会在后续已签名请求之前重新运行辅助程序。没有 `expiration` 时，凭证在
整条命令的生命周期内都是静态的。

首个可续期凭证会固定规范化身份。后续调用如果返回不同身份，会在它能够签名请求
之前被拒绝，因此后端切换了账号的辅助程序会失败退出，而不是在命令中途改变身份。

## OSS 传输

当 OSS 命令使用可续期凭证时，`ecctl` 通过仅绑定到 `127.0.0.1` 的短期凭证端点
和仅当前用户可读的临时 profile，让本地 `ossutil` 子进程获得访问权限。该端点使用
每条命令独有、无法猜测的路径；子进程退出后，端点和临时 profile 都会被删除，凭证
也永远不会出现在命令参数中。没有过期时间的 External AK 会作为本次操作的静态 AK
传给 OSS；可续期的 OSS broker 响应必须是带 security token 的 STS 凭证。

## 相关文档

- [Credentials URI](./credentials-uri.md)：对应的 HTTP 方式
- [凭证总览](./index.md)

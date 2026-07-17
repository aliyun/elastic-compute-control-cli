---
title: 配置
description: 配置 profile、凭证、地域、语言和输出。
---

# 配置

`ecctl configure` 写入资源命令使用的本地配置。它也可以读取兼容的本地 `aliyun` CLI 配置；当两者同时存在时，`ecctl` 配置会覆盖兼容配置中的同名值。

## 配置地域

```bash
ecctl configure set region cn-hangzhou
```

输出形态：

```json
{
  "key": "region",
  "profile": "default",
  "sensitive": false,
  "value": "cn-hangzhou"
}
```

设置默认输出模式：

```bash
ecctl configure set output json
```

读取当前生效的 profile：

```bash
ecctl configure get
```

输出形态：

```json
{
  "lang": "",
  "mode": "",
  "output": "json",
  "profile": "default",
  "region": "cn-hangzhou"
}
```

## 凭证

设置 AccessKey 凭证：

```bash
ecctl configure set access-key-id <id>
ecctl configure set access-key-secret <secret>
```

使用 STS 访问时，再设置安全令牌：

```bash
ecctl configure set security-token <token>
```

## 支持的配置项

列出支持的配置项：

```bash
ecctl configure list
```

当前配置项：

| 配置项 | 存储为 | 取值 |
|---|---|---|
| `region` | `region_id` | 任意语法合法的阿里云地域 ID |
| `access-key-id` | `access_key_id` | 字符串 |
| `access-key-secret` | `access_key_secret` | 字符串，敏感 |
| `security-token` | `sts_token` | 字符串，敏感 |
| `lang` | `language` | `en`、`zh-CN` |
| `output` | `output_format` | `json`、`text` |

敏感值默认掩码。仅在确实需要查看本地敏感值时使用 `--show-secret`。

## Profile

用 `--profile` 写入命名 profile：

```bash
ecctl --profile production configure set output json
```

profile 存在后切换当前 profile：

```bash
ecctl configure use production
```

`configure use` 会在兼容的 `aliyun` 配置和 `ecctl` 配置中检查该 profile 名，然后将所选 profile 记录到 `ecctl` 配置文件。

## 全局覆盖

全局 flag 可对单条命令覆盖配置：

```bash
ecctl --region cn-beijing --output json --lang en schema --list ecs
```

常用全局 flag：

| Flag | 用途 |
|---|---|
| `--profile` | 选择配置 profile |
| `--region` | 选择当前命令的阿里云地域 |
| `--output` | 选择 `json` 或 `text` 输出 |
| `--json` | 强制 JSON 输出 |
| `--lang` | 选择 `en` 或 `zh-CN` 文案 |
| `--no-color` | 关闭人类可读输出的颜色 |
| `--agent-envelope` | 将 JSON 输出包裹在 ecctl Agent envelope 中 |

## 环境变量

`ecctl` 识别以下环境变量覆盖：

| 变量 | 用途 |
|---|---|
| `ECCTL_PROFILE`、`ALIBABACLOUD_PROFILE`、`ALIBABA_CLOUD_PROFILE`、`ALICLOUD_PROFILE` | 未传 `--profile` 时的默认 profile |
| `ECCTL_REGION`、`ALIBABA_CLOUD_REGION_ID`、`ALIBABACLOUD_REGION_ID`、`ALICLOUD_REGION_ID` | 未传 `--region` 时的地域覆盖 |
| `ALIBABA_CLOUD_CONFIG_PATH`、`ALIBABACLOUD_CONFIG_PATH`、`ALICLOUD_CONFIG_PATH` | 兼容的 `aliyun` CLI 配置文件路径 |

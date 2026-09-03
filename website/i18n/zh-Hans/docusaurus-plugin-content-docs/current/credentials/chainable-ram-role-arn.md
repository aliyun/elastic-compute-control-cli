---
title: 链式 RAM 角色 ARN
description: 使用另一个命名 profile 作为源凭证来扮演 RAM 角色。
---

# 链式 RAM 角色 ARN

`ChainableRamRoleArn` 从一个源 *profile* 扮演 RAM 角色，而不是从内联凭证扮演。源 profile 会先按它自身使用的任意凭证模式完成解析，其凭证随后成为 `AssumeRole` 的源凭证。

这就是构建角色链的方式：一次 [OAuth](./oauth.md) 登录扮演共享账号中的角色，该角色再扮演工作负载账号中的角色。它同时把单一源凭证保存在一处，而不必把 access key 复制到每个下游 profile。

## 使用阿里云 CLI 配置

```bash
aliyun configure --mode ChainableRamRoleArn --profile workload
```

不支持 `ecctl configure --mode ChainableRamRoleArn`。`--mode` 只接受 `OAuth`。请把该 profile 写入 Aliyun-compatible 配置文件。

## profile 字段

| 字段 | 是否必需 | 说明 |
|---|---|---|
| `mode` | 实践中必需 | `ChainableRamRoleArn`。不会从其他字段推断 |
| `source_profile` | 是 | 同一个 Aliyun-compatible 文件中另一个 profile 的名称 |
| `ram_role_arn` | 是 | 完整 ARN：`acs:ram::<16位账号ID>:role/<角色名>` |
| `ram_session_name` | 否 | 未设置时 fallback 到 `ALIBABA_CLOUD_ROLE_SESSION_NAME` |
| `expired_seconds` | 否 | 请求的会话有效期（秒） |
| `policy` | 否 | 进一步收窄所扮演会话的内联策略 |
| `external_id` | 否 | 未设置时 fallback 到 `ALIBABA_CLOUD_EXTERNAL_ID` |
| `sts_endpoint` | 否 | 自定义 STS 端点。必须使用 HTTPS |
| `sts_region` | 否 | 地域 STS 端点选择 |
| `enable_vpc` | 否 | 使用 VPC STS 端点 |

该凭证模式没有环境变量形式。`source_profile` 指向一个 profile，而 profile 只存在于配置文件中。

从浏览器登录到工作负载账号的两跳角色链：

```json
{
  "current": "workload",
  "profiles": [
    {
      "name": "shared",
      "mode": "RamRoleArn",
      "access_key_id": "LTAI5t...",
      "access_key_secret": "...",
      "ram_role_arn": "acs:ram::1111111111111111:role/shared-admin",
      "region_id": "cn-hangzhou"
    },
    {
      "name": "workload",
      "mode": "ChainableRamRoleArn",
      "source_profile": "shared",
      "ram_role_arn": "acs:ram::2222222222222222:role/workload-deploy",
      "ram_session_name": "ecctl-session",
      "region_id": "cn-hangzhou"
    }
  ]
}
```

`source_profile` 必须指向同一个 Aliyun-compatible 配置文件中的 profile。源缺失时以 `source profile <name> not found` 失败；`source_profile` 为空时以 `source_profile is required for ChainableRamRoleArn` 失败。

## 循环检测

回到自身已经访问过的 profile 的角色链会在本地被拒绝：

```json
{
  "error": {
    "kind": "client",
    "code": "InvalidCredentials",
    "message": "credential profile chain contains a cycle at workload"
  }
}
```

检测覆盖整条解析路径，因此把自身作为间接源的 profile 会立即失败，而不会持续递归。

## ARN 校验与身份验证

与 [RAM 角色 ARN](./ram-role-arn.md) 相同，`ram_role_arn` 必须是完整的 `acs:ram::<16位账号ID>:role/<角色名>` ARN。`ecctl` 从中派生预期账号，并在第一条业务请求前通过官方 STS `GetCallerIdentity` 端点验证初始凭证。自定义 `sts_endpoint` 可以签发凭证，但永远不会被信任去验证自己签发的结果。

角色链中的每一跳都是可续期的，第一个可续期凭证会固定规范化身份。返回不同角色的刷新会在其签名请求前被拒绝。

## 验证

```bash
ecctl --profile workload configure get
ecctl --profile workload --region cn-hangzhou ecs region list
```

源跳失败时报告源 profile 自身的错误。扮演跳失败时报告 `refresh session token failed`，并附带 STS 响应体和一个 Request ID。

## 相关文档

- [RAM 角色 ARN](./ram-role-arn.md)：从内联凭证扮演 RAM 角色
- [凭证总览](./index.md)：解析顺序

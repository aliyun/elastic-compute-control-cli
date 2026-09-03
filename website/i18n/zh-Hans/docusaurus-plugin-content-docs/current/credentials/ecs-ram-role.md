---
title: ECS RAM 角色
description: 在 ECS 实例上通过实例元数据服务获取可续期凭证。
---

# ECS RAM 角色

`EcsRamRole` 从 ECS 实例元数据服务（IMDS）获取绑定到该实例的 RAM 角色的临时凭证。磁盘上不存储任何机密，凭证会自行续期，因此对于已经运行在 ECS 上的工作负载，这是合适的默认选择。

该模式只在已绑定角色的 ECS 实例上有效。在其他位置运行时，会因元数据服务不可达而失败。

## 使用阿里云 CLI 配置

```bash
aliyun configure --mode EcsRamRole --profile instance
```

不支持 `ecctl configure --mode EcsRamRole`。`--mode` 只接受 `OAuth`，而 ecctl 原生 profile 只能解析 OAuth 或静态凭证。请把该 profile 放在 Aliyun-compatible 配置文件中。

## 使用环境变量配置

```bash
export ALIBABA_CLOUD_ECS_METADATA=<role-name>
```

未选中任何已存储的 profile 时，或者 `ALIBABA_CLOUD_IGNORE_PROFILE=TRUE` 强制走纯环境变量路径时，会读取环境变量路径。在环境变量链中，它的检测顺序位于 AccessKey 密钥对之后、完整 OIDC 组合之后。

另外两个变量控制元数据服务本身：

| 变量 | 作用 |
|---|---|
| `ALIBABA_CLOUD_IMDSV1_DISABLED` | 为 true 时拒绝 fallback 到 IMDSv1，要求走加固的 IMDSv2 路径 |
| `ALIBABA_CLOUD_ECS_METADATA_DISABLED` | 为 true 时完全禁用元数据凭证获取 |

两者都按布尔值读取；`1` 和 `true` 均可，不区分大小写。

## profile 字段

| 字段 | 必填 | 说明 |
|---|---|---|
| `mode` | 否 | `EcsRamRole`。存在 `ram_role_name` 时推导得出 |
| `ram_role_name` | 否 | 为空时 fallback 到 `ALIBABA_CLOUD_ECS_METADATA` |

`ram_role_name` 可以留空。元数据服务能够报告已绑定的角色，因此空名称也能解析。提供名称更严格：当实例绑定的角色与 profile 期望的角色不一致时会失败。

```json
{
  "name": "instance",
  "mode": "EcsRamRole",
  "ram_role_name": "my-ecs-role",
  "region_id": "cn-hangzhou"
}
```

## 验证

```bash
ecctl --profile instance configure get
ecctl --profile instance --region cn-hangzhou ecs region list
```

请在实例本身上执行检查。从工作站执行时，命令失败在元数据获取环节而不是凭证有效性环节，这不是有用的信号。

## 续期

来自 IMDS 的凭证是可续期的。`ecctl` 在整条命令期间保留该 provider，并在凭证接近过期时于后续已签名请求之前刷新，因此长时间操作不会中途失败。

第一个可续期凭证会固定规范化身份。刷新后属于其他角色的凭证会在其签名请求之前被拒绝，因此实例上的角色变化无法在命令中途静默切换账号。

## 相关文档

- [OIDC](./oidc.md)：Kubernetes 及其他 OIDC 工作负载身份
- [RAM 角色 ARN](./ram-role-arn.md)：从源凭证扮演 RAM 角色
- [凭证总览](./index.md)

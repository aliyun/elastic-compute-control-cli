---
title: 资源专项优化
description: 按资源索引 ecctl 与直接 OpenAPI 调用不同的行为。
---

# 资源专项优化

[通用差异](../common-differences.md)说明 ecctl 资源命令共有的行为。本目录记录会改变调用方式或结果读取方式的资源专项转换和工作流。

每个产品页按资源组织内容。每个差异点先用文字说明，再给出具体的阿里云 CLI、
OpenAPI 和 ecctl 示例。适用条件和限制直接写在对应行为旁边。

## ECS

[ECS 专项优化](./ecs.md)包括镜像名称解析、安全组规则简写、期望状态更新、多 API
路由、按需详情查询和云助手输出解码。

## ACK

[ACK 专项优化](./ack.md)包括集群与节点池工作流、按需详情查询、kubeconfig 操作、
权限更新和版本元数据校验。

## VPC

[VPC 专项优化](./vpc.md)包括 VPC 和交换机的服务端 DryRun、幂等与列表输出归一化。

## 灵骏

[灵骏专项优化](./lingjun.md)包括集群扩缩容、按需节点查询、异步 VPD 创建和辅助网段管理。

所有条目仅覆盖以下命令报告的公开命令面：

```bash
ecctl capabilities --output json
ecctl schema --list
```

精确参数、默认值和命令契约请以 `ecctl schema` 或[资源参考](../../reference/resource-coverage.md)为准。

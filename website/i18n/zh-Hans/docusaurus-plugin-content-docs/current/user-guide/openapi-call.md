---
title: OpenAPI 调用
description: 对尚未建模为资源的操作使用 ecctl call。
---

# OpenAPI 调用

已有资源命令时优先使用资源命令。尚未进入公开资源命令面的阿里云 OpenAPI 操作，可以使用 `ecctl call`。

`ecctl call` 保留原始 OpenAPI 操作名和请求形态，不额外提供资源命令的 waiter、幂等注入或响应归一化。

## 列出产品

```bash
ecctl call --list --filter ecs --limit 3
```

输出：

```json
{
  "count": 1,
  "total": 1,
  "truncated": false,
  "filter": "ecs",
  "limit": 3,
  "products": [
    {
      "name": "ecs",
      "description": "Elastic Compute Service"
    }
  ]
}
```

## 查看操作

```bash
ecctl call --schema ecs DescribeInstances --generate-request
```

请求模板：

```json
{
  "RegionId": "<RegionId>"
}
```

## 调用操作

以 JSON 传入请求参数：

```bash
ecctl call ecs DescribeInstances --region cn-hangzhou --request '{"PageSize":10}'
```

或把 OpenAPI 参数作为 flags 传入：

```bash
ecctl call ecs DescribeInstances --region cn-hangzhou --PageSize 10
```

这些命令形态可被 `ecctl call` 接受；真正执行需要有效的阿里云凭证和地域。

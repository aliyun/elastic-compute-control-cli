---
title: 错误模型
description: 结构化错误字段、分类与退出行为。
---

# 错误模型

`ecctl` 把错误作为结构化 JSON 写入 `stdout`，因此调用方解析失败的方式与解析结果一致。
命令在出错时以非零状态退出。本页的约定由 `capabilities` 报告：

```bash
ecctl capabilities --output json
```

## 错误字段

失败信息放在 `error` 对象下。字段集合为：

| 字段 | 含义 |
|---|---|
| `kind` | 错误分类（见下文） |
| `code` | 稳定错误码 |
| `message` | 面向用户的消息 |
| `retryable` | 是否适合重试该命令 |
| `suggestion` | 面向人的建议 |
| `suggested_action` | 面向机器的下一步动作 |
| `field` | 相关输入字段（如适用） |
| `accepted_values` | `field` 的合法取值（如可用） |

当命令发起一次或多次阿里云 API 调用时，每次调用也会在 `actions` 下报告，
包含 `action_name`、`code` 和 `message`，从而在归一化的 `error` 之外保留
来源 `request_id` 和云端错误。

## 错误分类

`kind` 按来源对失败分组：

- `client` —— 在任何云调用之前请求就不合法，例如未知 schema 或缺少必填参数。
  原样重试无效。
- `not_found` —— 寻址的资源不存在。
- `service` —— 一次阿里云 API 调用失败。`actions` 条目携带每次调用的
  `request_id`、`code` 和 `message`。

## 示例

未知 schema 是 `client` 错误：

```bash
ecctl schema ecs.instance.frobnicate --brief
```

```json
{
  "error": {
    "kind": "client",
    "code": "UnknownSchema",
    "message": "schema command is not supported",
    "retryable": false,
    "suggestion": "Run `ecctl schema --list` to list supported schemas.",
    "suggested_action": "Run `ecctl schema --list` to list supported schemas."
  }
}
```

缺少必填参数同样是 `client` 错误，并指明缺失的参数：

```bash
ecctl ecs instance create --region cn-hangzhou
```

```json
{
  "error": {
    "kind": "client",
    "code": "MissingParameter",
    "message": "missing required parameters: --image, --sg, --type, --vswitch",
    "retryable": false,
    "suggestion": "Run the command with `--help` to see required parameters."
  }
}
```

读取一个不存在的资源是 `not_found` 错误：

```json
{
  "error": {
    "kind": "not_found",
    "code": "NotFound",
    "message": "vpc not found",
    "retryable": false
  }
}
```

## 在自动化中处理错误

使用 JSON 输出，按 `error.kind` 和 `error.code` 分支，并在重试前查看
`error.retryable`。当存在时，`error.field` 和 `error.accepted_values`
能准确指出需要修正的输入。

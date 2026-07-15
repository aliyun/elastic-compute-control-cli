---
title: 输出、语言和错误
description: 选择输出模式、语言和结构化错误处理方式。
---

# 输出、语言和错误

`ecctl` 默认面向 JSON。只有在人类阅读命令输出时才使用 `text`。

## 输出模式

JSON 输出：

```bash
ecctl capabilities --output json
```

Text 输出：

```bash
ecctl capabilities --output text
```

无视配置默认值强制 JSON：

```bash
ecctl --json capabilities
```

用 Agent envelope 包装 JSON：

```bash
ecctl --agent-envelope capabilities
```

## 语言

帮助和用户可见消息支持英文和简体中文：

```bash
ecctl --lang en --help
ecctl --lang zh-CN --help
```

持久化语言偏好：

```bash
ecctl configure set lang zh-CN
```

断言精确文案的脚本应显式传 `--lang`。

## 结构化错误

Capabilities 报告结构化错误写入 `stdout`，并可包含以下字段：

| 字段 | 含义 |
|---|---|
| `kind` | 错误类别 |
| `code` | 稳定错误码 |
| `message` | 用户可见消息 |
| `retryable` | 是否适合重试 |
| `suggestion` | 人类可读建议 |
| `suggested_action` | 面向机器的下一步动作 |
| `field` | 相关输入字段 |
| `accepted_values` | 可用值 |

自动化处理错误时使用 JSON 输出。错误分类与退出行为见
[错误模型](../reference/errors.md) 参考页。

## 颜色

禁用人类可读输出中的颜色：

```bash
ecctl --no-color --output text capabilities
```

将 `ECCTL_DISPLAY_MODE` 设置为 `AI` 或 `agent` 时，输出会变成更适合 Agent 解析的紧凑、无高亮格式；设置为 `Human` 时使用人类展示：JSON pretty-print，并对文本进行高亮。该值大小写不敏感，因此 `ai`、`Agent`、`AGENT` 也可用。

默认情况下，`ECCTL_DISPLAY_MODE` 是 `auto`：终端输出使用 `Human`，pipe、重定向和 CI 日志等非终端输出使用 `AI`。

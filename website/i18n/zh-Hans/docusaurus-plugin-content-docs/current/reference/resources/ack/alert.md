---
generated: true
generated_by: "website/scripts/gen-reference.mjs"
generated_command: "make build && npm --prefix website run gen:reference"
title: ack alert
sidebar_label: alert
description: "管理 ACK 报警规则状态及联系人组绑定"
---

# ack alert

管理 ACK 报警规则状态及联系人组绑定

运行 `ecctl ack alert <action> -h` 查看用法，或 `ecctl schema ack.alert.<action> --full` 获取该命令完整的结构化规格——每个参数与行为，便于 Agent 读取和填参。

## update

```bash
ecctl ack alert update [flags]
```

启动或停止 ACK 报警规则或规则集

- 类型：`mutation` · 风险：medium

| API | 调用时机 | 用途 |
|---|---|---|
| `StartAlert` | `--enabled` 等于 `true` 时 | 执行资源操作。 |
| `StopAlert` | `--enabled` 等于 `false` 时 | 执行资源操作。 |
| `UpdateContactGroupForAlert` | 指定 `--group-id` 时 | 执行资源操作。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--cluster` | string | ✓ | ACK 集群 ID |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--enabled` | boolean |  | 期望报警状态；使用 --enabled=true 启动，--enabled=false 停止 |
| `--group-id` | string_array |  | 需要绑定的报警联系人分组 ID |
| `--rule-id` | string |  | 报警规则 ID |
| `--ruleset-id` | string |  | 报警规则集 ID |

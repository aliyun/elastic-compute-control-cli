# Agent 友好 CLI 检测方案

本文定义一套针对任意 CLI 的检测方案，用来判断它是否符合 Agent 友好 CLI 的设计共识，以及 `ecctl` 在 [cli-design-rules.md](cli-design-rules.md) 中确立的 Agent-first CLI 规则。

检测对象可以是云 CLI、开发者工具、内部运维 CLI 或领域工具。默认采用黑盒检测：只通过命令行调用观察行为；如果能访问 schema、源码或测试环境，则可升级为灰盒/白盒检测。

## 1. 检测目标

检测要回答三个问题：

1. 这个 CLI 是否能被 Agent 稳定调用？
2. Agent 是否能不依赖长篇人类文档就理解命令、参数、输出和错误？
3. 在失败、重试、异步、分页、危险操作等场景下，CLI 是否给 Agent 足够信号恢复？

最终输出应该是一份报告：

- 总分与等级。
- 每个维度的通过、部分通过、失败、不可测。
- 关键阻断项。
- 可自动修复的建议。
- 需要人工产品决策的设计问题。

## 2. 检测模式

### 2.1 Discovery-only

只运行无副作用命令：

- `--help`
- `--version`
- `schema`
- `capabilities`
- `examples`
- 明确只读的 `list` / `get`

适合首次评估未知 CLI。

### 2.2 Read-only

在 Discovery-only 基础上，运行更多只读命令，并测试分页、字段裁剪、错误输出、无 TTY 行为。

适合已有凭证但不允许改资源的环境。

### 2.3 Safe-mutation

在隔离测试环境中创建和删除临时资源，检测：

- `create`
- `update`
- `delete`
- `--dry-run`
- `--no-wait`
- 幂等键
- waiter

适合云资源 CLI 或运维 CLI 的正式评测。

### 2.4 Contract-deep

结合 CLI schema、源码、测试用例或 OpenCLI 描述进行深度审计。

适合准备发布或作为 Agent 工具注册前的准入检查。

## 3. 检测输入

| 输入 | 必需 | 说明 |
| --- | --- | --- |
| CLI 可执行文件 | 是 | 例如 `ecctl`、`aws`、`kubectl` |
| 版本命令 | 否 | 默认尝试 `--version`、`version` |
| 根 help 命令 | 否 | 默认尝试 `--help`、`help` |
| 安全测试目录 | 是 | 用于运行命令、保存 stdout/stderr/report |
| 超时时间 | 是 | 默认每条命令 10s，mutation/wait 单独配置 |
| 凭证/profile | 否 | 只读或 mutation 检测时需要 |
| 只读样例命令 | 否 | 例如 `ecs instance list` |
| 安全 mutation 样例 | 否 | 例如创建临时 tag/resource |
| 期望 schema | 否 | 如果 CLI 提供 OpenCLI、JSON Schema 或自定义 schema |

如果缺少样例命令，检测器只能做根命令和 help 级别评估，不能证明资源操作是否 Agent 友好。

## 4. 检测原则

### 4.1 默认安全

检测器默认不得执行有真实副作用的命令。所有 mutation 检测必须满足至少一个条件：

- CLI 提供 `--dry-run`。
- 用户提供隔离测试环境。
- 用户明确提供可删除的测试资源。
- 命令本身是本地临时资源操作。

### 4.2 非交互

所有命令都必须在 stdin 关闭或接入空输入时执行：

```bash
timeout 10s <cli> <args> </dev/null
```

这里的 `timeout` 表示任意超时包装器；在 macOS 环境中可以使用 `gtimeout`，或由测试框架内置超时控制。

如果命令挂起、等待 prompt、进入 pager 或持续 spinner，应判定为非交互失败。

### 4.3 输出分流

检测必须分别捕获 stdout、stderr 和 exit code：

```text
stdout: 机器结果数据
stderr: 诊断、日志、进度
exit:   粗粒度成功/失败
```

禁止只看合并后的终端输出。Agent 真实调用时通常会分别读取这些通道。

### 4.4 黑盒结论要标注置信度

黑盒检测只能证明"观察到的命令表现符合/不符合"，不能证明所有命令都符合。报告必须区分：

- `pass`：样例命令通过。
- `fail`：样例命令失败。
- `partial`：部分命令通过，或只能人工判断。
- `unknown`：缺少样例或环境，不可测。

## 5. 评分模型

总分 100 分，分为 10 个维度：

| 维度 | 分值 |
| --- | ---: |
| 非交互默认 | 12 |
| 结构化输出 | 14 |
| stdout/stderr 通道分离 | 8 |
| 结构化错误 | 14 |
| 自描述与 schema | 12 |
| 有界输出 | 10 |
| 命名与参数一致性 | 8 |
| 幂等性与安全重试 | 8 |
| dry-run / explain / 风险预览 | 8 |
| 异步操作与 waiter | 6 |

等级：

| 等级 | 分数 | 含义 |
| --- | ---: | --- |
| A | 85-100 | Agent 友好，可作为 Agent 工具主路径 |
| B | 70-84 | 基本可用，有少量 wrapper 或 prompt 约束 |
| C | 50-69 | 可被 Agent 调用，但需要较多适配层 |
| D | 30-49 | 高风险，容易卡死、误判或解析失败 |
| F | 0-29 | 不适合直接给 Agent 调用 |

硬性降级规则：

- 任一常规命令在非 TTY 下挂起：最高 C。
- 只有人类表格输出，无 JSON 或稳定机器格式：最高 C。
- 错误只能靠散文字符串解析：最高 B。
- 危险 mutation 无 dry-run、无确认、无幂等：最高 C。
- 默认输出可能无界倾泻大量数据：最高 B。

## 6. 检测流程

### 6.1 命令发现

目标：判断 CLI 是否暴露可发现能力。

运行：

```bash
<cli> --version
<cli> version
<cli> --help
<cli> help
<cli> capabilities --output json
<cli> schema --list --output json
<cli> schema list --output json
<cli> schema --output json
```

检查项：

- 版本命令是否存在。
- 根 help 是否在 10s 内返回。
- help 是否包含高层命令结构。
- 是否存在机器可读 schema 或 capabilities。
- schema 是否能列出命令、参数、类型、枚举、输出和错误。

判定：

- 有 `schema` / `capabilities` 且输出可解析 JSON：高分。
- 只有自然语言 help：部分通过。
- help 很长、混乱、无命令层次：扣分。
- help 进入 pager 或等待交互：失败。

### 6.2 非交互检测

目标：确认 CLI 不会让 Agent 卡死。

对每个样例命令执行：

```bash
timeout 10s <cli> <args> </dev/null
```

覆盖场景：

- 根 help。
- 只读 list/get。
- 参数缺失。
- 错误枚举值。
- 需要确认的危险操作，必须使用 dry-run 或无效资源。

检查项：

- 是否超时。
- 是否等待确认 prompt。
- 是否进入交互式登录。
- 是否进入分页器。
- 是否在 stdin 无输入时明确失败并给出修复建议。

判定：

- 无 TTY 时主动失败并说明需要 `--yes` / `--force`：通过。
- 静默挂起：严重失败。
- 只有交互式登录，无 headless 凭证方式：失败。

### 6.3 结构化输出检测

目标：确认 Agent 可以稳定解析成功结果。

优先运行：

```bash
<cli> <read-command> --output json
<cli> <read-command> --json
<cli> <read-command> --machine
```

检查项：

- stdout 是否是合法 JSON。
- JSON 是否是对象，而不是裸字符串或混合文本。
- 是否包含稳定 envelope，例如 `ok`、`schema_version`、`result` / `items`。
- list 是否包含分页信息。
- JSON 字段是否扁平、语义明确。
- stdout 是否包含 ANSI 控制字符。

ANSI 检测：

```regex
\x1b\[[0-9;]*[A-Za-z]
```

判定：

- 默认或显式 JSON 且稳定 envelope：通过。
- 有 JSON 但缺少统一 envelope：部分通过。
- 只能输出表格、人类文本或颜色编码：失败。

### 6.4 通道分离检测

目标：确认 stdout 可被直接作为数据流消费。

运行正常命令和 verbose 命令：

```bash
<cli> <read-command> --output json
<cli> <read-command> --output json --verbose
```

检查项：

- stdout 是否只包含 JSON 数据。
- stderr 是否承载日志、warning、进度。
- verbose 是否没有污染 stdout。
- 设置 `NO_COLOR=1` 后是否禁用颜色。

判定：

- stdout 始终可 JSON parse：通过。
- stdout 偶尔混入 warning/progress：失败。
- stderr 中有诊断但不影响 stdout：通过。

### 6.5 结构化错误检测

目标：确认失败是可编程的。

构造错误：

```bash
<cli> <command> --unknown-flag
<cli> <command> --region invalid-region
<cli> <get-command> definitely-not-existing-id
<cli> <mutation-command> --dry-run --invalid-option
```

检查项：

- exit code 是否非零。
- stdout 或 stderr 是否包含可解析 JSON error。
- error 是否包含稳定字段：
  - `kind`
  - `code`
  - `message`
  - `field`
  - `accepted_values`
  - `retryable`
  - `suggested_action`
- 参数错误是否列出合法值。
- not found、conflict、timeout 是否可区分。

判定：

- 有稳定 error kind 和建议动作：通过。
- 只有 exit code 和散文错误：部分通过或失败。
- 错误码与实际原因错位：失败。
- dry-run 成功却 exit 非零：失败。

### 6.6 有界输出检测

目标：确认 CLI 不会吞掉 Agent 上下文。

运行集合命令：

```bash
<cli> <list-command> --output json
<cli> <list-command> --output json --limit 1
<cli> <list-command> --output json --fields id,name,status
```

检查项：

- list 是否有默认 limit。
- 是否有最大 limit。
- 是否返回 cursor / next token。
- 是否支持字段裁剪。
- 默认字段是否克制。
- 日志类命令是否有 `--tail`、`--since`、`--limit`。

判定：

- 默认 bounded 且可分页：通过。
- 支持 limit 但默认无界：部分通过。
- 无 limit、无分页、默认全量倾泻：失败。

### 6.7 命名与参数一致性检测

目标：判断 Agent 能否根据常见先验推断命令。

从 help/schema 抽取命令树和 flag，检查：

- 是否使用稳定结构，例如 `<product> <resource> <action>` 或 `<noun> <verb>`。
- 是否混用 `list` / `describe` / `ls` 表示同一语义。
- 是否混用 `delete` / `remove` / `destroy`。
- 同一 flag 是否跨命令语义一致。
- 是否存在一个 flag 同时表示 ID、名称、过滤表达式。
- 是否支持 canonical name，并把 alias 标记为 alias。

判定：

- 有固定 grammar 和动词表：通过。
- 少量历史 alias，但 schema 标明 canonical：部分通过。
- 命令命名完全扁平、API 名泄露、同义词混用：失败。

### 6.8 Dry-run 与 explain 检测

目标：确认 Agent 可以安全探索 mutation。

对 mutation 样例运行：

```bash
<cli> <mutation-command> --dry-run --output json
<cli> explain <command-id> --output json
```

检查项：

- dry-run 是否 exit 0 表示验证成功。
- dry-run 是否真的无副作用。
- 输出是否包含风险等级。
- 输出是否包含将调用的底层 API 或动作。
- 输出是否包含受影响资源。
- 输出是否提示真正执行所需确认参数。

判定：

- dry-run 可解释且无副作用：通过。
- dry-run 只做权限检查，不解释计划：部分通过。
- dry-run 语义反直觉或产生副作用：失败。

### 6.9 幂等性与重试检测

目标：确认 Agent 重试不会扩大副作用。

在安全测试环境中运行：

```bash
<cli> <create-command> --idempotency-key test-key --output json
<cli> <create-command> --idempotency-key test-key --output json
<cli> <create-command-with-different-params> --idempotency-key test-key --output json
```

检查项：

- 是否支持显式 idempotency key。
- 是否自动返回 idempotency key。
- 相同 key + 相同参数是否返回同一资源或等价结果。
- 相同 key + 不同参数是否返回 conflict。
- 可重试错误是否标记 `retryable=true`。
- `start` / `stop` 等状态操作是否幂等。

判定：

- mutation 幂等契约明确且可测：通过。
- 依赖底层云 API 幂等但 CLI 不暴露：部分通过。
- 重复执行会创建重复资源：失败。

### 6.10 异步与 waiter 检测

目标：确认 Agent 不需要自己写轮询循环。

在安全测试环境中运行：

```bash
<cli> <async-mutation> --output json
<cli> <async-mutation> --no-wait --output json
<cli> <wait-command> <id> --status <target> --output json
```

检查项：

- 异步 mutation 是否默认等待到收敛。
- `--no-wait` 是否返回 operation 信息。
- operation 是否包含 `target_state` 和 `poll_command`。
- wait 命令是否可复用。
- timeout 是否结构化返回。
- 失败状态是否可区分 conflict / timeout / not_found。

判定：

- 默认等待或返回清晰 poll command：通过。
- 只返回 request id，需要 Agent 猜状态 API：失败。

## 7. 自动化检测器建议

检测器可以采用以下数据结构描述评测目标：

```yaml
cli: ecctl
timeout: 10s
env:
  NO_COLOR: "1"
commands:
  version:
    - ["--version"]
    - ["version"]
  help:
    - ["--help"]
  schema:
    - ["schema", "--list", "--output", "json"]
  read:
    - ["ecs", "instance", "list", "--region", "cn-hangzhou", "--output", "json"]
  get_missing:
    - ["ecs", "instance", "get", "i-does-not-exist", "--region", "cn-hangzhou", "--output", "json"]
  mutation_dry_run:
    - ["ecs", "instance", "delete", "i-test", "--dry-run", "--yes", "--output", "json"]
  async_no_wait:
    - ["ecs", "instance", "start", "i-test", "--no-wait", "--output", "json"]
```

每条命令的原始结果应保存：

```json
{
  "command": ["ecctl", "ecs", "instance", "list", "--output", "json"],
  "exit_code": 0,
  "timed_out": false,
  "duration_ms": 842,
  "stdout": "{...}",
  "stderr": "",
  "stdout_json_valid": true,
  "stderr_json_valid": false,
  "ansi_in_stdout": false,
  "ansi_in_stderr": false
}
```

检测器不应该只保存最终分数。原始 stdout/stderr 是后续诊断和人工复核的证据。

## 8. 报告格式

推荐输出 JSON 和 Markdown 两种报告。

JSON 报告供 CI 或 Agent 消费：

```json
{
  "cli": "example-cli",
  "version": "1.2.3",
  "score": 76,
  "grade": "B",
  "summary": {
    "pass": 31,
    "partial": 8,
    "fail": 5,
    "unknown": 4
  },
  "dimensions": [
    {
      "name": "structured_output",
      "score": 10,
      "max_score": 14,
      "status": "partial",
      "findings": [
        "supports --output json",
        "missing schema_version in success output"
      ]
    }
  ],
  "blockers": [
    "delete command prompts in non-TTY mode"
  ],
  "recommendations": [
    "return ok=false JSON error when confirmation is required"
  ]
}
```

Markdown 报告供人类评审：

```markdown
# example-cli Agent 友好度检测报告

总分：76 / 100，等级：B

## 阻断项

- delete command prompts in non-TTY mode

## 维度评分

| 维度 | 分数 | 状态 |
| --- | ---: | --- |
| 结构化输出 | 10/14 | partial |
```

## 9. Agent 友好度检查清单

### P0 阻断项

- 常规命令在非 TTY 下不会挂起。
- Agent-facing 输出可解析为 JSON 或稳定机器格式。
- stdout 不混入日志、进度条、ANSI 或 warning。
- 失败时有可编程错误，而不只是散文。
- 危险 mutation 有 dry-run 或显式确认机制。
- list / logs / events 默认有输出边界。

### P1 关键项

- 有 schema、capabilities 或 OpenCLI 描述。
- 命令语法和动词表一致。
- 错误包含 kind、code、retryable、suggested_action。
- mutation 支持幂等键。
- 异步操作内置 waiter 或返回 poll command。
- 参数冲突在客户端侧结构化失败。

### P2 增强项

- 提供面向 Agent 的压缩 schema 摘要。
- 提供 examples 命令。
- 输出包含 `schema_version`。
- 支持 `--fields` 字段裁剪。
- schema diff 可用于兼容性检查。
- 报告 risk_level、actions_taken、request_id。

## 10. 常见问题与判定

### 10.1 只有 `--help`，没有 schema

判定为部分通过。人类可发现，但 Agent 需要解析自然语言，准确率和 token 成本不可控。

建议提供：

```bash
<cli> capabilities --output json
<cli> schema --list --output json
<cli> schema <command-id> --output json
```

### 10.2 默认表格，但支持 `--json`

如果 Agent 可稳定加 `--json`，判定为部分通过或通过，取决于 JSON 是否结构化、是否覆盖错误、是否无 ANSI。

如果 JSON 只覆盖成功输出，错误仍是散文，结构化错误维度扣分。

### 10.3 错误输出到 stderr 是否违规

不一定。通用 CLI 传统上错误走 stderr。检测时重点看两点：

- Agent 是否能稳定找到并解析错误对象。
- stdout 是否保持纯净。

对 `ecctl` 当前规则来说，错误写 stdout JSON；对任意 CLI 检测时，可接受 stderr JSON error，但需要在报告中标注与 `ecctl` 规则不完全一致。

### 10.4 CLI 没有 mutation

幂等、dry-run、waiter 维度标记为 `not_applicable`，不计入总分或按权重重分配。

### 10.5 CLI 是本地开发工具，不是云资源 CLI

仍然检测同一批原则：

- 非交互。
- 结构化输出。
- 通道分离。
- 结构化错误。
- 自描述。
- 有界输出。
- 一致命名。

mutation 可以映射为文件写入、构建产物生成、发布、删除、覆盖配置等本地副作用。

## 11. 最小可行检测集

如果只能做 15 分钟快速检测，至少运行：

```bash
timeout 10s <cli> --help </dev/null
timeout 10s <cli> --version </dev/null
timeout 10s <cli> <read-command> --output json </dev/null
timeout 10s <cli> <read-command> --output json --limit 1 </dev/null
timeout 10s <cli> <command> --unknown-flag </dev/null
NO_COLOR=1 timeout 10s <cli> <read-command> --output json </dev/null
```

快速判定：

- 会挂起：不适合 Agent。
- 无机器输出：需要 wrapper。
- 错误不可结构化：Agent 恢复能力弱。
- 输出无边界：上下文风险高。
- stdout 被污染：JSON parse 风险高。

## 12. 与 `ecctl` 设计规则的对应关系

| 检测维度 | 对应规则 |
| --- | --- |
| 非交互默认 | 输出规则、错误与状态、安全默认 |
| 命令语法 | 命令形态、Action 词表、命令面收敛规则 |
| 参数设计 | 参数规则、结构化入参分层 |
| 结构化输出 | 输出规则 |
| Exit code | 错误、退出码和状态 |
| 幂等性 | Spec-driven transition / ClientToken 注入 |
| Dry-run / explain | dry-run 退出码与 mutation 预检 |
| Waiter | 同步默认、内建 waiter |
| Schema 自省 | Spec-first、help/schema 生成 |
| 有界输出 | list `--limit`、分页规则 |
| 通道与日志 | stdout/stderr 分离 |


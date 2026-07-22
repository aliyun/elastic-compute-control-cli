---
title: 更新
description: 检查并安装 ecctl 发布版本，包括 Homebrew 安装。
---

# 更新

## 检查与安装

只检查最新公开版本，不修改当前安装：

```bash
ecctl update --check
```

安装最新版本：

```bash
ecctl update
```

两个命令都会返回结构化结果，展示当前版本、目标版本、是否存在更新，以及安装已经
完成还是仍在等待完成。

失败结果包含稳定的更新错误码和 `retryable` 标记。本地化的 `message` 用于展示，
`detail` 保留诊断原因，便于排障和自动化处理。

如需指定发布版本，传入不带 `v` 前缀的语义化版本号。降级或重新安装相同版本需要
`--force`：

```bash
ecctl update 0.2.0
ecctl update 0.2.0 --force
```

Homebrew 安装只能选择最新稳定版本；直接安装的二进制可以选择仍可下载的历史版本或
预发布版本。

## 校验与安装

安装前，ecctl 会校验发布元数据、校验和以及候选可执行文件。校验失败时会停止更新，
不会安装不可信或不完整的文件。

对于 macOS 和 Linux 上直接安装的二进制，ecctl 只会在校验完成后替换可执行文件；
如果安装后校验失败，则恢复旧版本。更新意外中断后，下次显式执行更新命令时会检查并
恢复安装状态。

Windows 不允许直接替换正在运行的可执行文件。ecctl 会启动辅助进程，返回
`update_pending: true` 和 `updated: false`；update 命令退出后继续完成替换。之后
显式执行 `ecctl update` 时，会报告未完成或失败的替换。早于首个支持 Windows
自更新版本的历史版本需要手动安装。

## Homebrew 安装

检测到受支持的 Homebrew 安装时，`ecctl update` 会通过对应的 Homebrew 完成更新，
无需先运行 `brew update`。

`--force` 会重新安装当前稳定版本。如果无法安全识别对应的 Homebrew，更新会返回
错误，不会直接覆盖由 Homebrew 管理的可执行文件。

## 自动版本检测

执行操作类命令时，ecctl 会定期检查是否存在新的稳定版本。建议性检查失败不会阻塞
原命令。更新提示只写入交互式终端的 stderr，同一版本每天最多一次，因此不会污染
JSON stdout。

在受控或离线环境中可关闭自动检测：

```bash
export ECCTL_DISABLE_UPDATE_CHECK=1
```

帮助、版本、补全和 update 命令也会执行自动检测。

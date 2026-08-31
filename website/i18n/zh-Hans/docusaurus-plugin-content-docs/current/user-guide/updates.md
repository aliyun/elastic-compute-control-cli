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

对于发布 updater v2 协议的版本，OSS 是主更新源。更新到最新稳定版时，ecctl 先读取
OSS 的 `version.txt` 指针，再下载该版本的 manifest 和 Sigstore bundle；指定版本时则
直接读取对应版本的 manifest，不访问最新版本指针。指定版本由版本目录名和签名
manifest 中的版本共同标识，不要求存在 `/<version>/version.txt` 对象。

Sigstore bundle 使用 ecctl 内置的可信根在本地验证。证书身份被严格限定为 GitHub
Actions OIDC issuer，以及本仓库 `main` 分支或匹配 SemVer 发布标签上的
`.github/workflows/release.yml`。
验证过程不会访问 GitHub、Sigstore、Rekor 或其他在线服务。只有原始 manifest 的签名
和身份验证通过后，ecctl 才会解析 manifest、校验 `checksums.txt`，并验证目标安装包的
SHA-256 摘要和大小。

可用性故障与完整性故障会被区别处理：

- OSS 无法连接、资产不存在，或原本有效的响应被提前中断时，可以进入兼容的不可变
  GitHub Release 回退路径。
- bundle 无效、签名身份不符、manifest 非法，以及校验和、大小或安装包不匹配都属于
  完整性错误。ecctl 会立即停止，禁止回退到其他来源。

没有 v2 manifest 的历史版本继续使用不可变 GitHub Release 元数据作为信任来源，
同时优先读取与其摘要一致的 OSS 资产。任何校验失败都会停止更新，不会安装不可信或
不完整的文件。

该协议保护发布身份和制品完整性，不保证镜像始终可用或绝对新鲜。镜像仍可能隐藏新
版本，或持续返回客户端已经见过的最高合法版本。ecctl 会检测低于本机历史已验证稳定
版本高水位的回退，但无法证明不存在尚未见过的新版本。

对于 macOS 和 Linux 上直接安装的二进制，ecctl 只会在校验完成后替换可执行文件；
如果安装后校验失败，则恢复旧版本。更新意外中断后，下次显式执行更新命令时会检查并
恢复安装状态。

Windows 不允许直接替换正在运行的可执行文件。ecctl 会启动辅助进程，返回
`update_pending: true` 和 `updated: false`；update 命令退出后继续完成替换。之后
显式执行 `ecctl update` 时，会报告未完成或失败的替换。早于首个支持 Windows
自更新版本的历史版本需要手动安装。

早于 updater v2 的客户端无法使用带签名的 OSS 独立更新路径，GitHub API 不可用时仍
可能更新失败。此时需要通过文档中的软件包、Homebrew 或直接下载方式，安装或重装首个
包含 updater v2 的 ecctl 版本。将来 Sigstore 可信根轮换且不再与旧客户端内置根重叠
时，过旧的 updater v2 客户端也可能需要按此方式引导升级。

## Homebrew 安装

检测到受支持的 Homebrew 安装时，`ecctl update` 会通过对应的 Homebrew 完成更新，
无需先运行 `brew update`。

`--force` 会重新安装当前稳定版本。如果无法安全识别对应的 Homebrew，更新会返回
错误，不会直接覆盖由 Homebrew 管理的可执行文件。

## 自动版本检测

执行操作类命令时，ecctl 会定期检查是否存在新的稳定版本。建议性检查失败不会阻塞
原命令。更新提示只写入交互式终端的 stderr，同一版本每天最多一次，因此不会污染
JSON stdout。

自动检测与显式更新检查使用相同的签名 v2 解析或不可变 GitHub 回退路径。缓存只保存
`verified_latest_version`，不会用较低的已验证版本覆盖它；升级后会忽略旧客户端写入的
未签名 `latest_version` 缓存字段。

在受控或离线环境中可关闭自动检测：

```bash
export ECCTL_DISABLE_UPDATE_CHECK=1
```

帮助、版本、补全和 update 命令也会执行自动检测。

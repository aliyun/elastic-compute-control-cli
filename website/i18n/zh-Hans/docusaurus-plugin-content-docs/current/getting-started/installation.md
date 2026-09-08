---
title: 安装
description: 用 Homebrew 安装 ecctl、下载预构建版本，或从源码构建。
---

# 安装

## 要求

- macOS 上推荐使用 Homebrew 安装。
- 仅从源码构建时需要 Go 1.25 或更高版本。
- 调用云 API 的命令需要阿里云凭证。
- 可选：已有兼容的 `aliyun` CLI 配置文件。

## 使用 Homebrew 安装

安装最新公开发布版本：

```bash
brew tap aliyun/ecctl https://github.com/aliyun/elastic-compute-control-cli
brew install --cask aliyun/ecctl/ecctl
ecctl --version
```

第一条命令会将当前仓库显式添加为 `aliyun/ecctl` Tap。
自 Homebrew 6.0 起，第三方 Tap 默认不受信任；使用全限定 cask 名安装只会信任该 cask 本身，无需单独执行 `brew trust`。

升级已有安装：

```bash
ecctl update
```

`ecctl update` 同时支持 Homebrew 和直接安装的二进制，无需先运行 `brew update`。
版本检查、指定版本和自动提醒见[更新](../user-guide/updates.md)。

## 下载预构建版本

从 [GitHub Releases](https://github.com/aliyun/elastic-compute-control-cli/releases)
下载与你的操作系统和架构对应的压缩包，解压后将 `ecctl` 放到 `PATH` 中。

验证安装：

```bash
ecctl --version
ecctl --help
```

## 启用 zsh 自动补全

安装命令和参数的自动补全：

```zsh
ecctl completion zsh
```

命令会将脚本保存到 `~/.zfunc/_ecctl`，并在 `~/.zshrc` 中添加加载配置段。
如果设置了 `ZDOTDIR`，这两个文件会使用该目录，而不是用户主目录。
已有 shell 设置和文件权限会保留，重复执行会更新配置，不会重复追加配置段。
如果 `ZDOTDIR` 已设置但为空，安装会报错；执行 `unset ZDOTDIR` 可改用用户主目录。

安装结果会显示文件位置和 `reload_command`。在当前终端执行该加载命令，
或打开新的 zsh 终端，即可启用补全。默认路径对应的加载命令是：

```zsh
source ~/.zfunc/_ecctl
```

脚本会在需要时初始化 zsh 补全系统。使用
`ecctl completion zsh --no-descriptions` 可省略候选项描述。
`ecctl completion zsh` 会直接安装补全，不再输出供重定向或进程替换使用的脚本。

## 从源码构建

克隆仓库并在根目录构建：

```bash
git clone https://github.com/aliyun/elastic-compute-control-cli.git
cd elastic-compute-control-cli
make build
```

二进制会写入 `bin/ecctl`。

验证：

```bash
./bin/ecctl --version
./bin/ecctl --help
```

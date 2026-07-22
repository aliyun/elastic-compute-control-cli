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
brew install ecctl
ecctl --version
```

第一条命令会将当前仓库显式添加为 `aliyun/ecctl` Tap。升级已有安装：

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

---
title: 安装
description: 用 Homebrew 或 Go 安装 ecctl，也可以从源码构建。
---

# 安装

## 要求

- 推荐安装方式需要 Homebrew，也可以使用 Go 1.25 或更高版本。
- 调用云 API 的命令需要阿里云凭证。
- 可选：已有兼容的 `aliyun` CLI 配置文件。

## 使用 Homebrew 安装

安装最新公开发布版本：

```bash
brew tap aliyun/ecctl https://github.com/aliyun/ecctl
brew install ecctl
```

## 使用 go install 安装

如果本地已安装 Go 1.25 或更高版本，可以执行：

```bash
go install github.com/aliyun/ecctl/cmd/ecctl@latest
```

二进制会安装到 `$(go env GOPATH)/bin`。确认该目录在 `PATH` 中后，验证：

```bash
ecctl --version
ecctl --help
```

你也可以从 [GitHub Releases](https://github.com/aliyun/ecctl/releases)
下载预构建压缩包。

## 从源码构建

从 checkout 构建时，在仓库根目录执行：

```bash
make build
```

二进制会写入 `bin/ecctl`。

验证：

```bash
./bin/ecctl --version
./bin/ecctl --help
```

---
title: 资源 Specs
description: 贡献者如何扩展资源命令覆盖。
---

# 资源 Specs

云资源行为优先声明在 YAML specs 中。Go hooks 只用于 spec schema 难以清晰表达的跨 API 派生或归一化。

## 布局

产品 spec：

```yaml
specs/<product>/product.yaml
```

资源 spec：

```yaml
specs/<product>/<resource>.yaml
```

来自本仓库的示例：

```yaml
specs/ecs/instance.yaml
specs/ecs/sg.yaml
specs/vpc/vpc.yaml
specs/vpc/vswitch.yaml
specs/ack/ack.yaml
```

## 生成 Catalog

资源 specs 会生成到：

```go
pkg/spec/catalog_generated.go
```

修改 specs 后运行：

```bash
make generate
```

当 `pkg/spec/catalog_generated.go` 变化时，需要一起提交。

## 参考页

**参考 → 按产品分的资源** 下的逐资源页面由 `ecctl schema` 生成，而非手写。
修改 spec 后，重新构建二进制并重新生成：

```bash
make build
npm --prefix website run gen:reference
```

提交重新生成的 Markdown：`website/docs/reference/resources/` 及其 `zh-Hans` 对应目录。

## 验证

优先使用项目目标：

```bash
make test
make lint
```

只修改 `website/` 下文档时，也运行：

```bash
cd website
npm run test
npm run typecheck
npm run build
```

## 外部 Specs

设置 `ECCTL_SPEC_DIR`，可以让发现和命令生成指向同布局的外部 spec 目录：

```bash
ECCTL_SPEC_DIR=/path/to/specs ecctl schema --list
```

这适合私有实验，或在合并 spec 前做验证。

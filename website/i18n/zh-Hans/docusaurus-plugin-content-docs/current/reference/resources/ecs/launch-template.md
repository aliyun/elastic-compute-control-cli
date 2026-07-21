---
generated: true
generated_by: "website/scripts/gen-reference.mjs"
generated_command: "make build && npm --prefix website run gen:reference"
title: ecs launch-template
sidebar_label: launch-template
description: "管理 ECS 启动模板"
---

# ecs launch-template

管理 ECS 启动模板

运行 `ecctl ecs launch-template <action> -h` 查看用法，或 `ecctl schema ecs.launch-template.<action> --full` 获取该命令完整的结构化规格——每个参数与行为，便于 Agent 读取和填参。

## create

```bash
ecctl ecs launch-template create [flags]
```

创建启动模板

- 类型：`mutation` · 风险：medium
- 同步：等待 `present`（waiter `template_visible`，超时 `300s`）；用 `--no-wait` 跳过等待。

| API | 调用时机 | 用途 |
|---|---|---|
| `CreateLaunchTemplate` | 每次执行命令时 | 执行资源操作。 |
| `DescribeLaunchTemplates` | 未指定 `--no-wait` 时 | 轮询等待资源达到目标状态。（重复调用） |
| `DescribeLaunchTemplates` | 未指定 `--no-wait` 时 | 读取资源视图。 |
| `DescribeLaunchTemplateVersions` | 未指定 `--no-wait` 时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--name` | string | ✓ | 启动模板名称 |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--description` | string |  | 启动模板中的实例描述 |
| `--image` | string |  | ECS 镜像 ID |
| `--keypair` | string |  | 密钥对名称 |
| `--resource-group` | string |  | 启动模板所属资源组 ID |
| `--resource-resource-group` | string |  | 从模板创建出的资源所属资源组 ID |
| `--resource-tag` | key_value |  | 从模板创建出的资源标签 key=value |
| `--security-groups` | array |  | 安全组 ID 列表 |
| `--sg` | string |  | 安全组 ID |
| `--tag` | key_value |  | 启动模板标签 key=value |
| `--type` | string |  | ECS 实例规格 |
| `--version-description` | string |  | 启动模板版本描述 |
| `--vswitch` | string |  | 交换机 ID |

## update

```bash
ecctl ecs launch-template update <id> [flags]
```

创建启动模板版本或切换默认版本

- 类型：`mutation` · 风险：medium
- 同步：等待 `present`（waiter `created_version_visible`，超时 `300s`）；用 `--no-wait` 跳过等待。

| API | 调用时机 | 用途 |
|---|---|---|
| `CreateLaunchTemplateVersion` | 指定 `--create-version` 时 | 执行资源操作。 |
| `DescribeLaunchTemplateVersions` | 指定 `--create-version` 且未指定 `--no-wait` 时 | 轮询等待资源达到目标状态。（重复调用） |
| `ModifyLaunchTemplateDefaultVersion` | 指定 `--default-version` 时 | 执行资源操作。 |
| `DescribeLaunchTemplateVersions` | 指定 `--default-version` 且未指定 `--no-wait` 时 | 轮询等待资源达到目标状态。（重复调用） |
| `DescribeLaunchTemplates` | 指定 `&lt;id>` 且未指定 `--no-wait` 时 | 读取资源视图。 |
| `DescribeLaunchTemplates` | 指定 `--name` 且未指定 `--no-wait` 且未指定 `&lt;id>` 时 | 读取资源视图。 |
| `DescribeLaunchTemplateVersions` | 指定 `--default-version` 且未指定 `--no-wait` 时 | 读取资源视图。 |
| `DescribeLaunchTemplateVersions` | 指定 `--create-version` 且未指定 `--no-wait` 时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--create-version` | boolean |  | 创建新的启动模板版本 |
| `--default-version` | integer |  | 启动模板默认版本号 |
| `--description` | string |  | 启动模板中的实例描述 |
| `--image` | string |  | ECS 镜像 ID |
| `--keypair` | string |  | 密钥对名称 |
| `--name` | string |  | 启动模板名称 |
| `--resource-resource-group` | string |  | 从模板创建出的资源所属资源组 ID |
| `--resource-tag` | key_value |  | 从模板创建出的资源标签 key=value |
| `--security-groups` | array |  | 安全组 ID 列表 |
| `--sg` | string |  | 安全组 ID |
| `--type` | string |  | ECS 实例规格 |
| `--version-description` | string |  | 启动模板版本描述 |
| `--vswitch` | string |  | 交换机 ID |

## delete

```bash
ecctl ecs launch-template delete <target> [flags]
```

删除启动模板或版本

- 类型：`mutation` · 风险：high

| API | 调用时机 | 用途 |
|---|---|---|
| `DescribeLaunchTemplates` | `&lt;target>` 以 `lt-` 开头时 | 读取资源视图。 |
| `DescribeLaunchTemplates` | 前序步骤未生成 `target_template` 时 | 读取资源视图。 |
| `DeleteLaunchTemplateVersion` | 指定 `--version` 时 | 执行资源操作。 |
| `DeleteLaunchTemplate` | 未指定 `--version` 时 | 执行资源操作。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--version` | integer |  | 启动模板版本号 |

## get

```bash
ecctl ecs launch-template get <target> [flags]
```

获取启动模板

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `DescribeLaunchTemplates` | `&lt;target>` 以 `lt-` 开头时 | 读取资源视图。 |
| `DescribeLaunchTemplates` | 前序步骤未生成 `target_template` 时 | 读取资源视图。 |
| `DescribeLaunchTemplateVersions` | 指定 `--with-versions` 时 | 读取资源视图。 |
| `DescribeLaunchTemplateVersions` | 指定 `--version` 且未指定 `--with-versions` 时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | 要包含的资源字段，使用逗号分隔 |
| `--version` | integer |  | 启动模板版本号 |
| `--with-versions` | boolean |  | 附带启动模板版本信息 |

## list

```bash
ecctl ecs launch-template list [<targets>...] [flags]
```

列出启动模板

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `DescribeLaunchTemplates` | 未指定 `&lt;targets>` 时 | 读取资源视图。 |
| `DescribeLaunchTemplates` | `&lt;targets>` 中包含以 `lt-` 开头的值时 | 读取资源视图。 |
| `DescribeLaunchTemplates` | `&lt;targets>` 中包含不以 `lt-` 开头的值时 | 读取资源视图。 |
| `DescribeLaunchTemplates` | `&lt;targets>` 中包含以 `lt-` 开头且尚未匹配的值时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | 要包含的资源字段，使用逗号分隔 |
| `--filter` | key_value |  | 过滤表达式 key=value |
| `--limit` | integer |  | 最多返回资源数量（默认：`100`） |
| `--page` | integer |  | 返回结果页码（默认：`1`） |

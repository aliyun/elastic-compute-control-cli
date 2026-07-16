---
title: ecs image
sidebar_label: image
description: "管理 ECS 镜像资源"
---

# ecs image

管理 ECS 镜像资源

运行 `ecctl ecs image <action> -h` 查看用法，或 `ecctl schema ecs.image.<action> --full` 获取该命令完整的结构化规格——每个参数与行为，便于 Agent 读取和填参。

## create

```bash
ecctl ecs image create [flags]
```

创建自定义镜像

- 类型：`mutation` · 风险：medium
- 同步：等待 `Available`（waiter `available_after_create`，超时 `1800s`）；用 `--no-wait` 跳过等待。
- 通过 `ClientToken` 幂等。
- 支持 `--dry-run` 校验。

| API | 调用时机 | 用途 |
|---|---|---|
| `CreateImage` | 每次执行命令时 | 执行资源操作。 |
| `DescribeImages` | 未指定 `--no-wait` 且未指定 `--dry-run` 时 | 轮询等待资源达到目标状态。（重复调用） |
| `DescribeImages` | 未指定 `--no-wait` 且未指定 `--dry-run` 时 | 返回最终资源视图。（复用等待结果，不额外请求） |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--architecture` | string |  | 镜像架构 |
| `--boot-mode` | string |  | 启动模式 |
| `--description` | string |  | 镜像描述 |
| `--detection-strategy` | string |  | 镜像检测策略 |
| `--disk-device-mapping` | object |  | 创建或导入镜像时使用的磁盘设备映射 |
| `--image-family` | string |  | 创建或更新镜像时关联的镜像族系 |
| `--image-version` | string |  | 镜像版本 |
| `--instance` | string |  | 用作镜像源的实例 ID |
| `--name` | string |  | 镜像名称 |
| `--platform` | string |  | 操作系统平台 |
| `--resource-group` | string |  | 资源组 ID |
| `--snapshot` | string |  | 用作镜像源的快照 ID |
| `--tag` | key_value |  | 标签赋值 key=value |

## update

```bash
ecctl ecs image update <id> [flags]
```

更新镜像属性或共享权限

- 类型：`mutation` · 风险：medium

| API | 调用时机 | 用途 |
|---|---|---|
| `ModifyImageAttribute` | 指定 `--name` 或指定 `--description` 或指定 `--image-family` 或指定 `--boot-mode` 或指定 `--license-type` 或指定 `--status-action` 或指定 `--features` 时 | 执行资源操作。 |
| `ModifyImageSharePermission` | 指定 `--share-add` 或指定 `--share-remove` 或指定 `--launch-permission` 时 | 执行资源操作。 |
| `DescribeImages` | 未指定 `--no-wait` 时 | 读取资源视图。 |
| `DescribeImageSharePermission` | （指定 `--share-add` 或指定 `--share-remove` 或指定 `--launch-permission`）且未指定 `--no-wait` 时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--boot-mode` | string |  | 启动模式 |
| `--description` | string |  | 镜像描述 |
| `--features` | object |  | 镜像功能开关 |
| `--image-family` | string |  | 创建或更新镜像时关联的镜像族系 |
| `--launch-permission` | string |  | 镜像启动权限设置 |
| `--license-type` | string |  | 许可证类型 |
| `--name` | string |  | 镜像名称 |
| `--share-add` | string_array |  | 加入镜像共享权限的账号 ID 列表 |
| `--share-remove` | string_array |  | 移除镜像共享权限的账号 ID 列表 |
| `--status-action` | string |  | 修改镜像时的目标状态 |

## delete

```bash
ecctl ecs image delete <id> [flags]
```

删除镜像

- 类型：`mutation` · 风险：high
- 同步：等待 `absent`（waiter `deleted_after_delete`，超时 `600s`）；用 `--no-wait` 跳过等待。

| API | 调用时机 | 用途 |
|---|---|---|
| `DeleteImage` | 每次执行命令时 | 执行资源操作。 |
| `DescribeImages` | 未指定 `--no-wait` 时 | 轮询等待资源达到目标状态。（重复调用） |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--force` | boolean |  | 即使被实例引用也强制删除镜像（必须显式指定）（默认：`false`） |

## get

```bash
ecctl ecs image get <id> [flags]
```

获取镜像

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `DescribeImageFromFamily` | 指定 `--family` 时 | 读取资源视图。 |
| `DescribeImages` | 指定 `&lt;id>` 时 | 读取资源视图。 |
| `DescribeImageSharePermission` | 指定 `--with-share-permission` 时 | 读取资源视图。 |
| `DescribeImageSupportInstanceTypes` | 指定 `--with-supported-instance-types` 时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--action-type` | string |  | 查询镜像支持实例规格时使用的操作类型 |
| `--family` | string |  | 镜像族系名称 |
| `--fields` | string |  | 要包含的资源字段，使用逗号分隔 |
| `--with-share-permission` | boolean |  | 附带镜像共享权限 |
| `--with-supported-instance-types` | boolean |  | 附带支持的实例规格 |

## list

```bash
ecctl ecs image list [<ids>...] [flags]
```

列出镜像资源

- 类型：`read` · 风险：low

| API | 调用时机 | 用途 |
|---|---|---|
| `DescribeImages` | 每次执行命令时 | 读取资源视图。 |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--fields` | string |  | 要包含的资源字段，使用逗号分隔 |
| `--filter` | key_value |  | 过滤表达式 key=value |
| `--limit` | integer |  | 最多返回资源数量（默认：`100`） |
| `--page` | integer |  | 返回结果页码（默认：`1`） |

## copy

```bash
ecctl ecs image copy <id> [flags]
```

跨地域复制镜像

- 类型：`mutation` · 风险：medium
- 同步：等待 `Available`（waiter `available_after_copy`，超时 `3600s`）；用 `--no-wait` 跳过等待。
- 通过 `ClientToken` 幂等。

| API | 调用时机 | 用途 |
|---|---|---|
| `CancelCopyImage` | 指定 `--cancel` 时 | 执行资源操作。 |
| `CopyImage` | 未指定 `--cancel` 时 | 执行资源操作。 |
| `DescribeImages` | 未指定 `--cancel` 且未指定 `--no-wait` 时 | 轮询等待资源达到目标状态。（重复调用） |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--region` | string | ✓ | Alibaba Cloud region |
| `--cancel` | boolean |  | 取消正在进行的复制镜像任务 |
| `--destination-description` | string |  | 复制镜像的目标描述 |
| `--destination-name` | string |  | 复制镜像的目标名称 |
| `--destination-region` | string |  | 复制镜像的目标地域 |
| `--encrypted` | boolean |  | 复制镜像时加密目标镜像 |
| `--kms-key-id` | string |  | 加密目标镜像使用的 KMS 密钥 ID |
| `--resource-group` | string |  | 资源组 ID |
| `--tag` | key_value |  | 标签赋值 key=value |

## export

```bash
ecctl ecs image export <id> [flags]
```

导出镜像到 OSS

- 类型：`mutation` · 风险：medium
- 同步：等待 `Finished`（waiter `task_finished`，超时 `3600s`）；用 `--no-wait` 跳过等待。

| API | 调用时机 | 用途 |
|---|---|---|
| `ExportImage` | 每次执行命令时 | 执行资源操作。 |
| `DescribeTaskAttribute` | 未指定 `--no-wait` 时 | 轮询等待资源达到目标状态。（重复调用） |
| `DescribeTaskAttribute` | 未指定 `--no-wait` 时 | 返回最终资源视图。（复用等待结果，不额外请求） |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--oss-bucket` | string | ✓ | 导出或导入镜像使用的 OSS 存储桶 |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--export-format` | string |  | 镜像导出格式 |
| `--image-format` | string |  | 镜像文件格式 |
| `--oss-prefix` | string |  | 导出镜像的 OSS 对象键前缀 |
| `--role-name` | string |  | 访问 OSS 使用的 RAM 角色名称 |

## import

```bash
ecctl ecs image import [flags]
```

从 OSS 导入镜像

- 类型：`mutation` · 风险：medium
- 同步：等待 `Available`（waiter `available_after_import`，超时 `3600s`）；用 `--no-wait` 跳过等待。
- 通过 `ClientToken` 幂等。

| API | 调用时机 | 用途 |
|---|---|---|
| `ImportImage` | 每次执行命令时 | 执行资源操作。 |
| `DescribeImages` | 未指定 `--no-wait` 时 | 轮询等待资源达到目标状态。（重复调用） |
| `DescribeImages` | 未指定 `--no-wait` 时 | 返回最终资源视图。（复用等待结果，不额外请求） |

| 参数 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `--disk-device-mapping` | object | ✓ | 创建或导入镜像时使用的磁盘设备映射 |
| `--region` | string | ✓ | Alibaba Cloud region |
| `--architecture` | string |  | 镜像架构 |
| `--boot-mode` | string |  | 启动模式 |
| `--description` | string |  | 镜像描述 |
| `--detection-strategy` | string |  | 镜像检测策略 |
| `--image-family` | string |  | 创建或更新镜像时关联的镜像族系 |
| `--image-version` | string |  | 镜像版本 |
| `--license-type` | string |  | 许可证类型 |
| `--name` | string |  | 镜像名称 |
| `--os-type` | string |  | 操作系统类型 |
| `--platform` | string |  | 操作系统平台 |
| `--resource-group` | string |  | 资源组 ID |
| `--role-name` | string |  | 访问 OSS 使用的 RAM 角色名称 |
| `--tag` | key_value |  | 标签赋值 key=value |

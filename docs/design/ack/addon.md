# ack addon

资源：集群组件

优先级：P0

本文件只描述 `ecctl ack addon` 的 interface 级命令设计：每个操作命令对应哪些 ACK API，不展开完整 flag、参数结构和输出结构；仅在多 API 分流时标明必要的特殊开关。

设计目标：覆盖集群组件安装、卸载、升级、配置和查询的 80% 主路径。组件存在两层概念，命令面通过 `--catalog` flag 在同一组 list/get 动作上分流，不引入额外动词：

- **catalog（平台目录）**：平台维护的可安装组件元信息，描述组件名、支持的集群类型、可用版本和参数 schema。catalog 与具体集群无关，通过 `addon list --catalog`（列出全部可装组件）和 `addon get --catalog --name <addon>`（查询单个组件元信息）暴露。
- **instance（集群已安装实例）**：某个集群上已安装的组件实例，承载实际版本、配置、运行状态和占用的 K8s 资源。`addon list` / `addon get` 默认指 instance；`create / update / delete / upgrade` 全部操作 instance。

版本管理、`update` 时的 config schema 校验、升级时的目标版本和依赖关系都由 catalog 中的组件元信息驱动；CLI 不维护本地组件版本表，必要时先 `list --catalog` 再操作 instance。异步 mutation（安装、卸载、升级）默认等待组件就绪并回读实例视图。

`upgrade` 已登记在 [cli-design-rules.md](../cli-design-rules.md) Action 词表，专门用于有任务模型的版本升级；这里采用 `addon upgrade` 而非 `addon update --version`，与 `cluster upgrade` / `nodepool upgrade` 命名对齐，便于 Agent 通过统一动词识别"组件版本升级"语义。

特殊开关命名是 interface 级建议，用来说明何时触发额外 API；最终字段名和参数细节在资源 spec 中定义。

## `ecctl ack addon create`

调用 API：

- [InstallClusterAddons](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-installclusteraddons)：在集群安装一个或多个组件。
- [GetClusterAddonInstance](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-getclusteraddoninstance)：回读组件实例状态。

注意事项：`create` 表达"在集群上安装组件"，因此使用 `InstallClusterAddons`，不再单独设计 `addon install` 子命令。`InstallClusterAddons` 接受组件名、版本、config 列表，单次可安装多个组件。组件版本和默认 config 来自 catalog，调用前可先查 `addon get --catalog --name <addon>` 校验版本和参数 schema。`InstallClusterAddons` 是异步接口，默认等待每个组件实例进入就绪态并回读实例视图。

## `ecctl ack addon update`

调用 API：

- [ModifyClusterAddon](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-modifyclusteraddon)：修改已安装组件的 config（YAML / JSON 配置）。
- [GetClusterAddonInstance](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-getclusteraddoninstance)：回读组件实例配置和状态。

注意事项：`update` 仅承载 config 编辑，不涉及版本变更；版本变更走 `addon upgrade`。config 的字段约束由 catalog 元信息中的参数 schema 驱动（见 `DescribeAddon`）。配置变更存在生效延迟时，默认等待组件实例回到就绪态并回读实例视图。

## `ecctl ack addon delete`

调用 API：

- [UnInstallClusterAddons](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-uninstallclusteraddons)：卸载组件。
- [ListClusterAddonInstances](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-listclusteraddoninstances)：确认组件实例已卸载。

注意事项：`delete` 表达"从集群移除已安装组件实例"，单次可卸载多个组件。`UnInstallClusterAddons` 是异步接口，默认等待组件实例不可见或进入卸载终态并回读列表。

## `ecctl ack addon get`

调用 API：

- 默认调用 [GetClusterAddonInstance](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-getclusteraddoninstance)：查询单个组件实例详情（版本、config、运行状态）。
- 指定 `--with-resources` 时调用 [ListClusterAddonInstanceResources](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-listclusteraddoninstanceresources)：查询组件实例占用的 K8s 资源列表。
- 指定 `--catalog --name <addon>` 时调用 [DescribeAddon](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-describeaddon)：查询平台目录中单个组件元信息（含支持版本列表、参数 schema、依赖关系）。

注意事项：默认查 instance 详情；`--with-resources` 按需追加查询关联的 K8s 资源（Deployment / DaemonSet / ConfigMap 等），避免 `get` 默认触发过多 API。`--catalog --name <addon>` 切换到平台目录视图，与 instance 详情通过同一动词分流，不独立设计 `addon catalog` 子命令。组件实例元信息查询统一走 `GetClusterAddonInstance`，不再使用 deprecated 的 `DescribeClusterAddonInstance` / `DescribeClusterAddonMetadata`。

## `ecctl ack addon list`

调用 API：

- 默认调用 [ListClusterAddonInstances](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-listclusteraddoninstances)：列出集群已安装组件实例。
- 指定 `--catalog` 时调用 [ListAddons](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-listaddons)：列出平台可安装组件目录，可按集群类型 / Kubernetes 版本过滤。

注意事项：默认指集群已安装 instance 列表；`--catalog` 切换到平台目录视图，与 instance 列表通过同一动词分流，不独立设计 `addon catalog list` 子命令。Agent 在执行 `addon create` / `addon upgrade` 前，应先通过 `addon list --catalog` 或 `addon get --catalog --name <addon>` 校验组件名和版本，并以参数 schema 驱动 config 字段填写。

## `ecctl ack addon upgrade`

调用 API：

- [UpgradeClusterAddons](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-upgradeclusteraddons)：升级组件实例到目标版本。
- [GetClusterAddonInstance](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-getclusteraddoninstance)：回读组件实例升级后的状态和版本。

注意事项：版本变更是组件管理的独立领域动作，因此设计为 `upgrade` 子命令而不是 `update --version`，与 `cluster upgrade` / `nodepool upgrade` 对齐。目标版本必须在 catalog 公布的支持版本范围内，调用前可先查 `addon get --catalog --name <addon>` 取得可升级版本。`UpgradeClusterAddons` 是异步接口，默认等待组件实例进入新版本就绪态并回读实例视图。

## 废弃/不推荐 API

以下 API 在阿里云官方文档已标记 deprecated，被新的 `Get*` / `List*` 系列 API 取代，`ecctl ack addon` 不接入；需要时通过 `ecctl aliyun cs <Op>` 兜底。

- [DescribeAddons](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-describeaddons)：已被 `ListAddons` 取代。
- [DescribeClusterAddonsVersion](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-describeclusteraddonsversion)：组件版本信息已合并进 `ListClusterAddonInstances` / `GetClusterAddonInstance`。
- [DescribeClusterAddonInstance](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-describeclusteraddoninstance)：已被 `GetClusterAddonInstance` 取代。
- [DescribeClusterAddonMetadata](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-describeclusteraddonmetadata)：组件元信息已被 `DescribeAddon` 取代。
- [DescribeClusterAddonsUpgradeStatus](https://help.aliyun.com/zh/ack/ack-managed-and-ack-dedicated/developer-reference/api-cs-2015-12-15-describeclusteraddonsupgradestatus)：升级状态查询统一走 `GetClusterAddonInstance` 与 `ack task get`。

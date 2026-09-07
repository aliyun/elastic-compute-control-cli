# 使用 ecctl 管理 Sandbox 和模板

本文面向 Sandbox 功能试用者，适用于附带本文的分支试用包。后端兼容范围核对于 2026-09-07，具体提交号、构建时间和平台见包内 `BUILD-INFO.json`，也可运行 `./ecctl --version` 查看。正式版安装渠道可能尚未包含这些功能。

`ecctl sbx` 是 `ecctl sandbox` 的别名。它包含两个资源：Sandbox 实例和 template 模板。模板用于定义可重复创建的运行环境，Sandbox 是从模板启动、带有生命周期的实例。这里的 `sbx` 与阿里云 AgentRun 产品下的资源使用不同接口。

## 1. 解压并确认版本

按运行 CLI 的电脑选择试用包：Apple Silicon Mac 使用 `darwin_arm64`，Linux x86_64 使用 `linux_amd64`。CLI 的平台与远端 Sandbox 的镜像架构是两件事。

```bash
# 填入压缩包文件名中的版本标识，然后进入解压目录。
PACKAGE_VERSION='<试用包版本>'
tar -xzf "ecctl_${PACKAGE_VERSION}_darwin_arm64.tar.gz"
cd "ecctl_${PACKAGE_VERSION}_darwin_arm64"
./ecctl --version
./ecctl sbx --help
./ecctl sbx template --help
```

下文在此目录执行，使用 `./ecctl`，避免误调用 PATH 中的旧版本。示例使用 Bash 和 `jq`；macOS 的默认 Shell 如为 zsh，可先运行 `bash`。`jq` 用于从 JSON 中提取资源 ID。

分发时请同时提供 `checksums.txt`，在压缩包所在目录用 macOS 的 `shasum -a 256 -c checksums.txt` 或 Linux 的 `sha256sum -c checksums.txt` 校验。Mac 试用包没有 Apple 公证；若系统阻止运行，核验来源及校验值后，在系统“隐私与安全性”中允许该程序。

试用包不会通过 `ecctl update` 分发。更新试用版应替换为新的试用包，避免切回尚未包含 Sandbox 功能的正式版本。

## 2. 选择后端并配置连接

Sandbox 命令从环境变量读取连接配置，不使用阿里云 AK、`ecctl configure` 的账号配置或 kubeconfig 登录。

| 环境变量 | 用途 |
| --- | --- |
| `ECCTL_SANDBOX_BACKEND` | `e2b`、`fc`、`acs` 或 `auto`。建议试用时显式选择。 |
| `E2B_API_KEY` | 对应后端的项目或管理密钥。不是阿里云 AccessKey，也不读取旧变量 `E2B_ACCESS_TOKEN`。 |
| `E2B_API_URL` | 完整 API 地址，优先级高于 `E2B_DOMAIN`。 |
| `E2B_DOMAIN` | 未指定 API URL 时，拼出 `https://api.<domain>`。默认 `e2b.app`。 |
| `ECCTL_SANDBOX_CA_FILE` | 可选的 PEM CA 文件，追加到系统信任池，仍校验域名。 |

先在 Bash 中交互输入密钥，避免把真实密钥写进命令历史或共享文件：

```bash
read -r -s -p 'Sandbox API Key: ' E2B_API_KEY
printf '\n'
export E2B_API_KEY
```

然后从下面三组配置中选择一组。切换后端时同时替换密钥和端点，并清除不再需要的 CA 配置。

### E2B 官方服务或自托管 E2B

```bash
export ECCTL_SANDBOX_BACKEND=e2b
export E2B_DOMAIN=e2b.app
export E2B_API_URL=https://api.e2b.app
unset ECCTL_SANDBOX_CA_FILE
```

自托管 E2B 将域名、API URL 和密钥换成集群提供的值。它需要实现当前 CLI 使用的 API 路径，不能仅凭“兼容 E2B”判断支持所有操作。

### 阿里云 FC 云沙箱

```bash
export ECCTL_SANDBOX_BACKEND=fc
export E2B_DOMAIN=cn-beijing.e2b.fc.aliyuncs.com
export E2B_API_URL=https://api.cn-beijing.e2b.fc.aliyuncs.com
unset ECCTL_SANDBOX_CA_FILE
```

使用 FC 控制台创建的云沙箱 API Key。示例为北京地域，端点、模板和 Sandbox 必须属于同一地域。`--region` 不会替换这里的 API 端点。地域和账号约束见 [FC 使用约束](https://help.aliyun.com/zh/functioncompute/usage-constraints-of-fc-agent-sandbox)。

### ACS Agent Sandbox

```bash
export ECCTL_SANDBOX_BACKEND=acs
export E2B_DOMAIN=sandbox.example.com
export E2B_API_URL=https://api.sandbox.example.com
# 仅在集群使用私有 CA 时设置，文件由集群管理员提供。
export ECCTL_SANDBOX_CA_FILE=/absolute/path/to/ca.pem
```

替换为集群实际域名，`E2B_API_KEY` 使用 sandbox-manager 的管理密钥。集群必须已安装相关控制器、sandbox-manager，并准备可用 SandboxSet。ecctl 不会自动安装组件，也不会从 `~/.kube/config` 或 Deployment 中提取密钥。集群准备步骤见 [ACS 接入文档](https://help.aliyun.com/zh/cs/user-guide/connect-to-agent-sandbox-using-the-e2b-sdk)。

本次实测采用原生协议的 `api.<domain>` 控制面。ACS 私有协议的路径入口没有纳入本次实测，不应由原生协议通过推断其可用。

所有远程 API URL 都必须使用 HTTPS。仅字面量环回 IP，如 `http://127.0.0.1:18080`，允许 HTTP；`http://localhost` 和远程 HTTP 地址会被拒绝。没有关闭 TLS 校验的开关。

`auto` 只识别 FC 主机名及其 CNAME 链，否则使用原生 E2B 路径，不自动识别 ACS。使用自定义域名、代理或端口转发时应显式选择后端。选择后端只改变兼容行为，不会修改请求目标。

## 3. 创建第一个 Sandbox

先列出并选择一个可用模板：

```bash
./ecctl sbx template list --limit 20
TEMPLATE_ID='<从 templates 数组中选择的 id>'
./ecctl sbx template get "$TEMPLATE_ID"
```

FC 的内置模板包括 `base` 和 `code-interpreter-v1`，以目标账号实际可用模板为准。E2B 和 ACS 的模板 ID 由各自环境决定，不要直接跨后端复用。[FC 内置模板说明](https://help.aliyun.com/zh/functioncompute/built-in-templates/)

创建一个存活 5 分钟的 Sandbox，并记录返回的 ID：

```bash
RUN_ID="trial-$(date +%Y%m%d%H%M%S)"
./ecctl sbx create --template "$TEMPLATE_ID" \
  --timeout-seconds 300 --timeout 180s \
  --metadata "{\"trial_id\":\"$RUN_ID\"}" > sandbox-created.json

# 仅在上一步退出码为 0 时继续。
SBX_ID=$(jq -er '.sandbox.id' sandbox-created.json)
./ecctl sbx get "$SBX_ID"
./ecctl sbx list --filter "metadata.trial_id=$RUN_ID"
```

默认输出为 JSON，创建成功的资源在 `.sandbox` 中。`--timeout-seconds 300` 设置远端存活时间，`--timeout 180s` 设置本次 CLI 操作的等待预算，单位和用途不同。默认会等待实例进入 `running`，使用 `--no-wait` 时需自行查询状态。

延长存活时间、暂停和恢复：

```bash
./ecctl sbx update "$SBX_ID" --timeout-seconds 600
# FC 账号需已开通暂停/恢复白名单；ACS 需集群具备对应能力。
./ecctl sbx pause "$SBX_ID"
./ecctl sbx get "$SBX_ID"
./ecctl sbx resume "$SBX_ID" --timeout-seconds 600
```

`resume` 只恢复控制面状态，不打开终端。`sbx` 当前没有 `exec`、SSH、PTY、文件上传下载或 `run_code` 命令，需要这些功能时使用对应后端支持的 E2B SDK。

用完后显式释放：

```bash
./ecctl sbx delete "$SBX_ID" --timeout 300s
./ecctl sbx list --filter "metadata.trial_id=$RUN_ID"
```

删除默认等到 API 查询确认不存在才成功，最长 300 秒。`--no-wait` 只表示删除请求已被接受。删除超时或查询失败会返回非零，不能视为已清理完成。

## 4. 创建和管理模板

### E2B：从镜像创建模板

以下命令走 E2B 模板构建 API，要求后端提供相应服务。本分支尚未完成原生 E2B 模板的账号实测。

```bash
./ecctl sbx template create --name trial-python \
  --from-image python:3.12 \
  --cpu-count 2 --memory-mb 1024 \
  --step '{"type":"RUN","args":["python -m pip install requests"]}' \
  --timeout 15m > template-created.json

# 仅在创建成功后继续。
TEMPLATE_ID=$(jq -er '.template.id' template-created.json)
./ecctl sbx template get "$TEMPLATE_ID"
./ecctl sbx create --template "$TEMPLATE_ID" --timeout-seconds 300 > from-template.json
CREATED_ID=$(jq -er '.sandbox.id' from-template.json)
./ecctl sbx get "$CREATED_ID"
./ecctl sbx delete "$CREATED_ID"
```

CLI 会先申请模板和构建 ID，再提交构建计划，默认等待 `ready`。`--from-image` 与 `--from-template` 二选一；后者用于从已有模板继续构建。`--step` 可重复，每项为结构化构建步骤或 `@file`。格式依据 [E2B OpenAPI](https://github.com/e2b-dev/E2B/blob/main/spec/openapi.yml)。

ecctl 不解析 Dockerfile，不上传本地 COPY/ADD 上下文。需要复杂 Dockerfile 或本地文件时，先在外部构建并推送镜像，再使用 `--from-image`，或使用官方模板工具。镜像能力和构建流程见 [E2B 模板入门](https://docs.e2b.dev/template/quickstart)。

私有镜像使用 `--from-image-registry @registry.json`，文件结构如下。用真实凭证替换占位符后设为仅本人可读，不随试用包分发：

```json
{"type":"registry","username":"<registry-user>","password":"<registry-password>"}
```

构建状态、日志及可见性管理示例：

```bash
BUILD_ID='<template get 返回的 builds[].build_id>'
./ecctl sbx template build-status "$TEMPLATE_ID" --build-id "$BUILD_ID"
./ecctl sbx template build-logs "$TEMPLATE_ID" --build-id "$BUILD_ID" --limit 100
./ecctl sbx template update "$TEMPLATE_ID" --public=false
# 只有确实需要公开模板时才执行 publish。
./ecctl sbx template publish "$TEMPLATE_ID"
./ecctl sbx template unpublish "$TEMPLATE_ID"
./ecctl sbx template tag-list "$TEMPLATE_ID"
./ecctl sbx template tag-assign --name trial-python:default --tag stable
./ecctl sbx template tag-delete --name trial-python --tag stable
# 确认不再需要此模板及其后续创建能力后执行。
./ecctl sbx template delete "$TEMPLATE_ID"
```

若创建在申请模板之后失败，检查错误中附带的恢复命令，先查询并处理已申请的模板再重试。`--no-wait` 会返回已申请的 `.template.id`、`.template.build_id` 和等待状态，随后可用 `build-status` 跟踪。

### FC：从符合要求的镜像创建模板

FC 官方支持模板构建、标签和别名，但本分支未完成这些操作的完整 FC 实测。下面是 ecctl 已实现的镜像构建入口，试用时先选目标地域支持的镜像：

```bash
FC_IMAGE='<目标地域可拉取、符合 FC 要求的镜像地址>'
./ecctl sbx template create --name trial-fc-python \
  --from-image "$FC_IMAGE" --cpu-count 2 --memory-mb 2048 \
  --timeout 15m
./ecctl sbx template list --limit 20
```

远端镜像须包含 `linux/amd64`，即使 CLI 运行在 Apple Silicon Mac 上也如此。使用自有 ACR EE 时，需满足账号、地域和 VPC 拉取要求；官方示例镜像有单独路径。具体约束见 [FC 自定义镜像模板](https://help.aliyun.com/zh/functioncompute/build-a-custom-image-template)。

本版 `template create` 没有 `X-E2B-Template-*` 扩展 Header 输入，也不配置 FC builder 的目标仓库、VPC、交换机或安全组。需要这些扩展的构建，请使用 FC 官方 SDK/工具先建好模板，再用 ecctl 查询和创建 Sandbox。`--from-image-registry` 是 E2B 标准构建字段，不等同于 FC 扩展 Header。

### ACS：基础模板来自 SandboxSet，快照模板由 ecctl 创建

ACS 不能使用 `ecctl sbx template create` 创建 SandboxSet。管理员按 [ACS 的预热池配置步骤](https://help.aliyun.com/zh/cs/user-guide/connect-to-agent-sandbox-using-the-e2b-sdk) 准备 `sandboxset.yaml`，通过 Kubernetes 创建：

```bash
kubectl --kubeconfig "$HOME/.kube/config" apply -f sandboxset.yaml
kubectl --kubeconfig "$HOME/.kube/config" get sandboxsets -n default
./ecctl sbx template list --limit 20
```

SandboxSet 就绪并被 sandbox-manager 识别后，其模板 ID 才可用于 `sbx create`。模板的镜像、规格和预热池由 Kubernetes 管理。ecctl 的 `template delete` 也不能删除这种基础模板。

如果集群已配置 checkpoint 驱动，可以从运行中的 Sandbox 创建快照模板并恢复。先按第 3 节创建并保留一个运行中的 `$SBX_ID`，然后执行：

```bash
./ecctl sbx snapshot "$SBX_ID" --snapshot-name "$RUN_ID-snapshot" > snapshot.json
SNAPSHOT_ID=$(jq -er '.sandbox.snapshot_template' snapshot.json)
./ecctl sbx create --template "$SNAPSHOT_ID" \
  --timeout-seconds 300 --timeout 300s > restored.json
RESTORED_ID=$(jq -er '.sandbox.id' restored.json)
./ecctl sbx get "$RESTORED_ID"

./ecctl sbx delete "$RESTORED_ID"
./ecctl sbx template delete "$SNAPSHOT_ID"
./ecctl sbx delete "$SBX_ID"
```

每一步失败都应先处理错误再继续。快照 ID 按 API 返回值原样使用，不等同于 Kubernetes Checkpoint 名称。该流程在 sandbox-manager v0.6.6 的管理服务入口实测通过，源 Sandbox 在快照后仍保持运行。

公网 ALB 曾在恢复约 60 秒时返回 504，绕过 ALB 后恢复约 76 秒成功。`--timeout 300s` 无法扩大 ALB 的超时预算，管理员需按 [AlbConfig 监听器配置](https://help.aliyun.com/zh/cs/user-guide/alb-ingress-configuration-dictionary) 调整请求及空闲超时后复验。端口转发成功不代表公网恢复已通过。

## 5. 后端操作范围速查

“已实现”指 ecctl 已具备命令和协议映射，不等于该账号实测通过。ACS 的“实测”限于 sandbox-manager v0.6.6 的测试环境，不覆盖所有可选参数。FC 的平台边界依据 [FC E2B 兼容说明](https://help.aliyun.com/zh/functioncompute/e2b-compatibility-explanation)。

| 操作或参数 | 原生 E2B / 自托管 E2B | FC | ACS 兼容模式 |
| --- | --- | --- | --- |
| Sandbox create/get/list/delete | 已实现，账号实测待完成 | 官方支持，本版未做完整 FC 回归 | 已实测 |
| `update --timeout-seconds` | 已实现 | 官方支持 | 已实测 |
| `pause` / `resume` | 已实现，受服务能力限制 | 需账号白名单 | 已实测，依赖集群能力 |
| `snapshot` / 从快照创建 | 已实现，受服务能力限制 | 官方暂不兼容 | 已实测管理服务入口，公网恢复受 ALB 超时限制 |
| `refresh` / `fork` | 已实现，需对应 API | 未确认支持，勿作为试用前提 | CLI 提前拒绝 |
| `logs` | 已实现 | 官方说明返回空数组 | CLI 提前拒绝 |
| `metrics` | 已实现 | CPU/内存可用，磁盘/页缓存占位值不能用作真实指标 | CLI 提前拒绝 |
| `update --network` | 已实现，需服务支持 | 官方说明成功响应不产生实际变更 | CLI 提前拒绝，包括 TTL 与 network 混合更新 |
| `create --network` | 已实现，需服务支持 | 未逐项确认 | CLI 提前拒绝 |
| `create --volume-mounts` | 已实现，依赖服务与存储配置 | Volume 暂不兼容 | CLI 提前拒绝 |
| template get/list | 已实现，原生服务端分页 | 列表已适配兼容路径及本地分页 | 已实测，列表本地分页 |
| template create | 已实现，镜像或已有模板构建 | 官方支持镜像构建，本版完整链路待实测，扩展 Header 未接入 | CLI 拒绝，基础模板通过 SandboxSet 创建 |
| template delete | 已实现 | 官方支持自建模板管理，权限由服务端判断 | 快照删除已实测，不能删除 SandboxSet 模板 |
| template update/publish/unpublish | 已实现 | 本版可调用，具体可见性语义待实测 | CLI 提前拒绝 |
| template build-status/build-logs | 已实现 | 官方支持模板构建，本版待完整实测 | CLI 提前拒绝 |
| template tag-list/tag-assign/tag-delete | 已实现 | 官方支持标签，本版待完整实测 | CLI 提前拒绝 |

FC 的限制目前没有像 ACS 一样全部做本地拦截。命令存在、HTTP 200 或空数组不能证明功能生效，尤其不要把 FC 网络更新当作已应用的网络策略。FC 的 `--from-template` 和各类构建步骤也未逐项验证。

所有后端都没有通过本资源接入 Team、API Key、独立 Volume 管理、交互式终端或文件 API。存活时长、CPU/内存、并发、模板构建等配额由服务及账号决定，CLI 的参数可解析不代表服务端会接受。原生 E2B 的本分支实测记录仍待补齐，自托管环境还要确认部署版本提供所需接口。

## 6. 常见问题

| 现象 | 检查方式 |
| --- | --- |
| `unknown command sbx` | 用 `./ecctl --version` 确认正在运行试用包，检查是否误用了旧版本。 |
| `MissingCredential` / 401 | 检查当前进程的 `E2B_API_KEY` 是否属于目标后端，不能用 kubeconfig 或阿里云 AK 替代。 |
| `InvalidSandboxBackend` | 后端只接受 `auto`、`e2b`、`fc`、`acs`。 |
| 模板列表 404 | 检查 API URL、显式后端、地域和路由，不能靠反复切换密钥解决路径不兼容。 |
| TLS 证书错误 | 核对证书域名、证书链和 CA 文件，不能用 CA 文件绕过域名错误。 |
| `UnsupportedOperation` | 对照上表，ACS 会在发送受限请求前拒绝。 |
| `UnsupportedACSTemplateDeletion` | 目标是不能由兼容 API 删除的基础模板，需走集群管理流程。 |
| `InvalidResponse` | 服务返回了与接口不符的结构。构建状态、构建日志、标签及 Sandbox 详情不会再被错误归一化为成功空结果。 |
| 创建超时或 504 | 请求可能已在服务端执行，先按本次 metadata 查询，再决定清理或重试。CLI 不会自动重试创建。 |
| 分页 token 不可用 | FC/ACS token 与后端和 API 端点绑定，切换后端或端点后从第一页重新查询。 |

FC/ACS 的模板分页每页重新获取完整模板列表，再按 ID 排序。它不是一致性快照，不适合用旧 token 推断并发新增或删除的模板总量。

反馈问题时附上 `./ecctl --version`、后端类型、去除敏感信息的命令、错误码和 RequestId，并说明是否经过代理或 ALB。不要附 API Key、私有仓库密码或 kubeconfig。

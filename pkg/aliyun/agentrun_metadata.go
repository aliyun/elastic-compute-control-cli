package aliyun

import (
	"encoding/json"
	"strings"
)

// AgentRun is present in the pinned aliyun-openapi-meta product catalog, but
// that module does not yet ship its 2025-09-10 version manifest or operation
// detail files. Keep this narrowly scoped compatibility snapshot aligned with
// the official AgentRun SDK until the upstream metadata package publishes the
// same files.
func manualOpenAPIMetadata(language, path string) ([]byte, bool, error) {
	const prefix = "/agentrun/"
	if !strings.HasPrefix(strings.ToLower(path), prefix) {
		return nil, false, nil
	}

	operations := agentRunSandboxOpenAPIOperations(language)
	if strings.EqualFold(path, prefix+"version.json") {
		apis := make(map[string]openAPINewAPI, len(operations))
		for name, operation := range operations {
			apis[name] = openAPINewAPI{
				Title:   operation.title,
				Summary: operation.summary,
			}
		}
		content, err := json.Marshal(openAPINewVersion{
			Version: "2025-09-10",
			Style:   "ROA",
			APIs:    apis,
		})
		return content, true, err
	}

	name := strings.TrimSuffix(strings.TrimPrefix(path, prefix), ".json")
	operation, ok := operations[name]
	if !ok {
		return nil, false, nil
	}
	content, err := json.Marshal(openAPINewDetail{
		Name:        name,
		Protocol:    "HTTPS",
		Method:      operation.method,
		PathPattern: operation.path,
		Parameters:  operation.parameters,
	})
	return content, true, err
}

type agentRunSandboxOpenAPIOperation struct {
	title      string
	summary    string
	method     string
	path       string
	parameters []openAPINewRequestParameter
}

func agentRunSandboxOpenAPIOperations(language string) map[string]agentRunSandboxOpenAPIOperation {
	text := func(en, zh string) string {
		if strings.HasPrefix(strings.ToLower(language), "zh") {
			return zh
		}
		return en
	}
	parameter := func(name, description, position, paramType string, required bool, children ...openAPINewRequestParameter) openAPINewRequestParameter {
		return openAPINewRequestParameter{
			Name:          name,
			Description:   description,
			Position:      position,
			Type:          paramType,
			Required:      required,
			SubParameters: children,
		}
	}
	body := func(children ...openAPINewRequestParameter) openAPINewRequestParameter {
		return parameter("body", text("Request body", "请求体"), "Body", "Struct", true, children...)
	}
	path := func(name, en, zh string) openAPINewRequestParameter {
		return parameter(name, text(en, zh), "Path", "String", true)
	}
	query := func(name, en, zh, paramType string) openAPINewRequestParameter {
		return parameter(name, text(en, zh), "Query", paramType, false)
	}
	bodyField := func(name, en, zh, paramType string, required bool) openAPINewRequestParameter {
		return parameter(name, text(en, zh), "Body", paramType, required)
	}

	templateName := path("templateName", "Template name", "模板名称")
	sandboxID := path("sandboxId", "Sandbox ID", "沙箱 ID")
	templateBody := []openAPINewRequestParameter{
		bodyField("allowAnonymousManage", "Whether anonymous data-plane sandbox management is allowed", "是否允许数据链路管理沙箱", "Boolean", false),
		bodyField("armsConfiguration", "ARMS configuration", "ARMS 配置", "Struct", false),
		bodyField("containerConfiguration", "Container configuration", "容器配置", "Struct", false),
		bodyField("cpu", "CPU cores", "CPU 核数", "Float", true),
		bodyField("credentialConfiguration", "Credential configuration", "凭证配置", "Struct", false),
		bodyField("description", "Template description", "模板描述", "String", false),
		bodyField("diskSize", "Disk size", "磁盘大小", "Integer", false),
		bodyField("enableAgent", "Whether the Sandbox Agent is enabled", "是否启用 Sandbox Agent", "Boolean", false),
		bodyField("enablePreStop", "Whether pre-stop is enabled", "是否启用停止前处理", "Boolean", false),
		bodyField("environmentVariables", "Environment variables", "环境变量", "Struct", false),
		bodyField("executionRoleArn", "Execution role ARN", "执行角色 ARN", "String", false),
		bodyField("logConfiguration", "Log configuration", "日志配置", "Struct", false),
		bodyField("memory", "Memory in MB", "内存大小，单位 MB", "Integer", true),
		bodyField("nasConfig", "NAS mount configuration", "NAS 挂载配置", "Struct", false),
		bodyField("networkConfiguration", "Network configuration", "网络配置", "Struct", true),
		bodyField("ossConfiguration", "OSS mount configurations", "OSS 挂载配置", "RepeatList", false),
		bodyField("preStopTimeoutInSeconds", "Pre-stop timeout in seconds", "停止前处理超时时间，单位秒", "Integer", false),
		bodyField("sandboxIdleTimeoutInSeconds", "Sandbox idle timeout in seconds", "沙箱空闲超时时间，单位秒", "Integer", false),
		bodyField("sandboxTTLInSeconds", "Deprecated sandbox TTL in seconds", "已弃用的沙箱存活时间，单位秒", "Integer", false),
		bodyField("scalingConfig", "Scaling configuration", "弹性配置", "Struct", false),
		bodyField("templateConfiguration", "Template-type-specific configuration", "模板类型相关配置", "Struct", false),
		bodyField("templateName", "Template name", "模板名称", "String", true),
		bodyField("templateType", "Template type", "模板类型", "String", true),
		bodyField("workspaceId", "Workspace ID", "工作空间 ID", "String", false),
	}
	updateTemplateBody := make([]openAPINewRequestParameter, 0, len(templateBody)-2)
	for _, field := range templateBody {
		if field.Name == "templateName" || field.Name == "templateType" || field.Name == "diskSize" {
			continue
		}
		field.Required = false
		updateTemplateBody = append(updateTemplateBody, field)
	}

	return map[string]agentRunSandboxOpenAPIOperation{
		"CreateTemplate": {
			title: text("Create template", "创建模板"), summary: text("Creates a sandbox template.", "创建沙箱模板。"),
			method: "POST", path: "/2025-09-10/templates", parameters: []openAPINewRequestParameter{body(templateBody...)},
		},
		"UpdateTemplate": {
			title: text("Update template", "更新模板"), summary: text("Updates a sandbox template.", "更新沙箱模板。"),
			method: "PUT", path: "/2025-09-10/templates/{templateName}", parameters: []openAPINewRequestParameter{
				templateName,
				query("clientToken", "Idempotency token", "幂等令牌", "String"),
				body(updateTemplateBody...),
			},
		},
		"DeleteTemplate": {
			title: text("Delete template", "删除模板"), summary: text("Deletes a sandbox template.", "删除沙箱模板。"),
			method: "DELETE", path: "/2025-09-10/templates/{templateName}", parameters: []openAPINewRequestParameter{templateName},
		},
		"GetTemplate": {
			title: text("Get template", "获取模板"), summary: text("Gets a sandbox template.", "获取沙箱模板。"),
			method: "GET", path: "/2025-09-10/templates/{templateName}", parameters: []openAPINewRequestParameter{templateName},
		},
		"ListTemplates": {
			title: text("List templates", "列出模板"), summary: text("Lists sandbox templates.", "列出沙箱模板。"),
			method: "GET", path: "/2025-09-10/templates", parameters: []openAPINewRequestParameter{
				query("templateType", "Template type filter", "模板类型过滤条件", "String"),
				query("pageNumber", "Page number", "页码", "Integer"),
				query("pageSize", "Page size", "每页数量", "Integer"),
				query("status", "Template status filter", "模板状态过滤条件", "String"),
				query("templateName", "Template name filter", "模板名称过滤条件", "String"),
				query("workspaceId", "Workspace ID filter", "工作空间 ID 过滤条件", "String"),
				query("workspaceIds", "Workspace IDs filter", "工作空间 ID 列表过滤条件", "String"),
			},
		},
		"ActivateTemplateMCP": {
			title: text("Enable template MCP", "启用模板 MCP"), summary: text("Enables the MCP service for a sandbox template.", "启用沙箱模板的 MCP 服务。"),
			method: "PATCH", path: "/2025-09-10/templates/{templateName}/mcp/activate", parameters: []openAPINewRequestParameter{
				templateName,
				body(
					bodyField("enabledTools", "Enabled MCP tools", "启用的 MCP 工具", "RepeatList", false),
					bodyField("transport", "MCP transport", "MCP 传输协议", "String", false),
				),
			},
		},
		"StopTemplateMCP": {
			title: text("Disable template MCP", "停用模板 MCP"), summary: text("Stops the MCP service for a sandbox template.", "停止沙箱模板的 MCP 服务。"),
			method: "PATCH", path: "/2025-09-10/templates/{templateName}/mcp/stop", parameters: []openAPINewRequestParameter{templateName},
		},
		"CreateSandbox": {
			title: text("Create sandbox", "创建沙箱"), summary: text("Creates a sandbox from a template.", "基于模板创建沙箱。"),
			method: "POST", path: "/2025-09-10/sandboxes", parameters: []openAPINewRequestParameter{body(
				bodyField("nasConfig", "NAS configuration", "NAS 配置", "Struct", false),
				bodyField("ossMountConfig", "OSS mount configuration", "OSS 挂载配置", "Struct", false),
				bodyField("polarFsConfig", "PolarFS configuration", "PolarFS 配置", "Struct", false),
				bodyField("sandboxId", "Requested sandbox ID", "指定的沙箱 ID", "String", false),
				bodyField("sandboxIdleTimeoutInSeconds", "Sandbox idle timeout in seconds", "沙箱空闲超时时间，单位秒", "Integer", false),
				bodyField("sandboxIdleTimeoutSeconds", "Deprecated sandbox idle timeout in seconds", "已弃用的沙箱空闲超时时间，单位秒", "Integer", false),
				bodyField("templateName", "Template name", "模板名称", "String", true),
			)},
		},
		"DeleteSandbox": {
			title: text("Delete sandbox", "删除沙箱"), summary: text("Deletes a sandbox.", "删除沙箱。"),
			method: "DELETE", path: "/2025-09-10/sandboxes/{sandboxId}", parameters: []openAPINewRequestParameter{sandboxID},
		},
		"GetSandbox": {
			title: text("Get sandbox", "获取沙箱"), summary: text("Gets a sandbox.", "获取沙箱。"),
			method: "GET", path: "/2025-09-10/sandboxes/{sandboxId}", parameters: []openAPINewRequestParameter{sandboxID},
		},
		"ListSandboxes": {
			title: text("List sandboxes", "列出沙箱"), summary: text("Lists sandboxes.", "列出沙箱。"),
			method: "GET", path: "/2025-09-10/sandboxes", parameters: []openAPINewRequestParameter{
				query("templateName", "Template name filter", "模板名称过滤条件", "String"),
				query("templateType", "Template type filter", "模板类型过滤条件", "String"),
				query("maxResults", "Maximum results", "最大返回数量", "Integer"),
				query("nextToken", "Pagination token", "分页令牌", "String"),
				query("sandboxId", "Sandbox ID filter", "沙箱 ID 过滤条件", "String"),
				query("status", "Sandbox status filter", "沙箱状态过滤条件", "String"),
			},
		},
		"StopSandbox": {
			title: text("Stop sandbox", "停止沙箱"), summary: text("Stops a sandbox.", "停止沙箱。"),
			method: "POST", path: "/2025-09-10/sandboxes/{sandboxId}/stop", parameters: []openAPINewRequestParameter{sandboxID},
		},
	}
}

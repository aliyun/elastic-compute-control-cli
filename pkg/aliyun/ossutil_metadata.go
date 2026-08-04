package aliyun

import (
	"sort"
	"strings"
)

const ossUtilProductCode = "oss"

type ossUtilParameterMetadata struct {
	OpenAPIParameter
	flag string
}

type ossUtilOperationMetadata struct {
	command    string
	mutation   bool
	summary    map[string]string
	parameters []ossUtilParameterMetadata
}

var ossUtilOperations = map[string]ossUtilOperationMetadata{
	"PutBucket": {
		command:  "put-bucket",
		mutation: true,
		summary: map[string]string{
			"en": "Create an OSS bucket.",
			"zh": "创建 OSS Bucket。",
		},
		parameters: []ossUtilParameterMetadata{
			ossUtilParameter("Bucket", "bucket", "The name of the bucket.", "Path", "String", true),
			ossUtilParameter("ACL", "acl", "The access control list of the bucket.", "Header", "String", false),
			ossUtilParameter("ResourceGroupId", "resource-group-id", "The resource group ID of the bucket.", "Header", "String", false),
			ossUtilParameter("CreateBucketConfiguration", "create-bucket-configuration", "The storage class and data redundancy configuration.", "Body", "Object", false),
		},
	},
	"GetBucketInfo": {
		command: "get-bucket-info",
		summary: map[string]string{
			"en": "Query information about an OSS bucket.",
			"zh": "查询 OSS Bucket 信息。",
		},
		parameters: []ossUtilParameterMetadata{
			ossUtilParameter("Bucket", "bucket", "The name of the bucket.", "Path", "String", true),
		},
	},
	"GetBucketAcl": {
		command: "get-bucket-acl",
		summary: map[string]string{
			"en": "Query an OSS bucket ACL and determine whether the bucket exists.",
			"zh": "查询 OSS Bucket ACL，并判断 Bucket 是否存在。",
		},
		parameters: []ossUtilParameterMetadata{
			ossUtilParameter("Bucket", "bucket", "The name of the bucket.", "Path", "String", true),
		},
	},
	"ListBuckets": {
		command: "list-buckets",
		summary: map[string]string{
			"en": "List OSS buckets owned by the current account.",
			"zh": "列举当前账号拥有的 OSS Bucket。",
		},
		parameters: []ossUtilParameterMetadata{
			ossUtilParameter("Prefix", "prefix", "The prefix that bucket names must contain.", "Query", "String", false),
			ossUtilParameter("Marker", "marker", "The bucket name after which listing starts.", "Query", "String", false),
			ossUtilParameter("MaxKeys", "max-keys", "The maximum number of buckets to return.", "Query", "Integer", false),
			ossUtilParameter("ResourceGroupId", "resource-group-id", "The resource group ID of the buckets.", "Query", "String", false),
			ossUtilParameter("TagKey", "tag-key", "The bucket tag key.", "Query", "String", false),
			ossUtilParameter("TagValue", "tag-value", "The bucket tag value.", "Query", "String", false),
			ossUtilParameter("Tagging", "tagging", "The bucket tag filter.", "Query", "String", false),
		},
	},
	"ListObjects": {
		command: "list-objects-v2",
		summary: map[string]string{
			"en": "List objects in an OSS bucket.",
			"zh": "列举 OSS Bucket 中的对象。",
		},
		parameters: []ossUtilParameterMetadata{
			ossUtilParameter("Bucket", "bucket", "The name of the bucket.", "Path", "String", true),
			ossUtilParameter("ContinuationToken", "continuation-token", "The token from which the listing continues.", "Query", "String", false),
			ossUtilParameter("Delimiter", "delimiter", "The character used to group object names.", "Query", "String", false),
			ossUtilParameter("EncodingType", "encoding-type", "The encoding type of object names in the response.", "Query", "String", false),
			ossUtilParameter("FetchOwner", "fetch-owner", "Whether to include object owner information in the response.", "Query", "Boolean", false),
			ossUtilParameter("MaxKeys", "max-keys", "The maximum number of objects to return.", "Query", "Integer", false),
			ossUtilParameter("Prefix", "prefix", "The prefix that object names must contain.", "Query", "String", false),
			ossUtilParameter("StartAfter", "start-after", "The object name after which the listing starts.", "Query", "String", false),
		},
	},
	"DeleteObject": {
		command:  "delete-object",
		mutation: true,
		summary: map[string]string{
			"en": "Delete an object from an OSS bucket.",
			"zh": "删除 OSS Bucket 中的对象。",
		},
		parameters: []ossUtilParameterMetadata{
			ossUtilParameter("Bucket", "bucket", "The name of the bucket.", "Path", "String", true),
			ossUtilParameter("Key", "key", "The name of the object.", "Path", "String", true),
			ossUtilParameter("VersionId", "version-id", "The version ID of the object to delete.", "Query", "String", false),
			ossUtilParameter("BypassGovernanceRetention", "bypass-governance-retention", "Whether to bypass governance retention for the object.", "Header", "Boolean", false),
		},
	},
	"DeleteBucket": {
		command:  "delete-bucket",
		mutation: true,
		summary: map[string]string{
			"en": "Delete an empty OSS bucket.",
			"zh": "删除空的 OSS Bucket。",
		},
		parameters: []ossUtilParameterMetadata{
			ossUtilParameter("Bucket", "bucket", "The name of the bucket.", "Path", "String", true),
		},
	},
}

func ossUtilParameter(name, flag, description, position, parameterType string, required bool) ossUtilParameterMetadata {
	return ossUtilParameterMetadata{
		OpenAPIParameter: OpenAPIParameter{
			Name:        name,
			Description: description,
			Position:    position,
			Type:        parameterType,
			Required:    required,
		},
		flag: flag,
	}
}

func ossUtilOperationNames() []string {
	names := make([]string, 0, len(ossUtilOperations))
	for name := range ossUtilOperations {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func ossUtilProduct(lang string) OpenAPIProduct {
	name := "Object Storage Service"
	if strings.HasPrefix(strings.ToLower(lang), "zh") {
		name = "对象存储 OSS"
	}
	return OpenAPIProduct{
		Code:      ossUtilProductCode,
		Name:      name,
		Version:   "v2",
		Style:     "OSS",
		APINames:  ossUtilOperationNames(),
		Endpoints: map[string]OpenAPIEndpoint{},
	}
}

func ossUtilOperationSummary(lang string, operation string) (OpenAPIOperationSummary, bool) {
	metadata, ok := ossUtilOperations[operation]
	if !ok {
		return OpenAPIOperationSummary{}, false
	}
	key := "en"
	if strings.HasPrefix(strings.ToLower(lang), "zh") {
		key = "zh"
	}
	return OpenAPIOperationSummary{
		Title:   operation,
		Summary: metadata.summary[key],
	}, true
}

func ossUtilOperationDetail(operation string) (OpenAPIOperationDetail, bool) {
	metadata, ok := ossUtilOperations[operation]
	if !ok {
		return OpenAPIOperationDetail{}, false
	}
	parameters := make([]OpenAPIParameter, 0, len(metadata.parameters))
	for _, parameter := range metadata.parameters {
		parameters = append(parameters, parameter.OpenAPIParameter)
	}
	return OpenAPIOperationDetail{
		Name:       operation,
		Protocol:   "HTTPS",
		Style:      "OSS",
		Parameters: parameters,
	}, true
}

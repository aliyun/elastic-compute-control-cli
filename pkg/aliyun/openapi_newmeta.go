package aliyun

import (
	"encoding/json"
	"strings"

	aliyunopenapimeta "github.com/aliyun/aliyun-openapi-meta"
)

type openAPINewProductSet struct {
	Products []openAPINewProduct `json:"products"`
}

type openAPINewProduct struct {
	Code         string                        `json:"code"`
	Name         string                        `json:"name"`
	Version      string                        `json:"version"`
	EndpointType string                        `json:"endpointType"`
	Endpoints    map[string]openAPINewEndpoint `json:"endpoints"`
}

type openAPINewEndpoint struct {
	RegionID string `json:"regionId"`
	Name     string `json:"regionName"`
	Public   string `json:"public"`
	VPC      string `json:"vpc"`
}

type openAPINewVersion struct {
	Version string                   `json:"version"`
	Style   string                   `json:"style"`
	APIs    map[string]openAPINewAPI `json:"apis"`
}

type openAPINewAPI struct {
	Title      string `json:"title"`
	Summary    string `json:"summary"`
	Deprecated bool   `json:"deprecated"`
}

type openAPINewDetail struct {
	Name        string                       `json:"name"`
	Deprecated  bool                         `json:"deprecated"`
	Protocol    string                       `json:"protocol"`
	Method      string                       `json:"method"`
	PathPattern string                       `json:"pathPattern"`
	Parameters  []openAPINewRequestParameter `json:"parameters"`
}

type openAPINewRequestParameter struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Position    string `json:"position"`
	Type        string `json:"type"`
	Required    bool   `json:"required"`
}

func readOpenAPINewAPI(language string, code string, name string) (*openAPINewAPI, error) {
	content, err := readOpenAPINewMetadata(language, "/"+strings.ToLower(code)+"/version.json")
	if err != nil {
		return nil, err
	}
	var version openAPINewVersion
	if err := json.Unmarshal(content, &version); err != nil {
		return nil, err
	}
	api, ok := version.APIs[name]
	if !ok {
		return nil, nil
	}
	return &api, nil
}

func readOpenAPINewAPIDetail(language string, code string, name string) (*openAPINewDetail, error) {
	content, err := readOpenAPINewMetadata(language, "/"+strings.ToLower(code)+"/"+name+".json")
	if err != nil {
		return nil, err
	}
	var detail openAPINewDetail
	if err := json.Unmarshal(content, &detail); err != nil {
		return nil, err
	}
	return &detail, nil
}

func readOpenAPINewMetadata(language string, path string) ([]byte, error) {
	prefix := "zh-CN"
	if language == "en" {
		prefix = "en-US"
	}
	return aliyunopenapimeta.Metadatas.ReadFile(prefix + path)
}

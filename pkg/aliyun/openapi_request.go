package aliyun

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
)

type openAPIRequest struct {
	Product      string
	Version      string
	ApiName      string
	RegionId     string
	Domain       string
	Scheme       string
	Method       string
	PathPattern  string
	Style        string
	EndpointType string

	QueryParams map[string]string
	FormParams  map[string]string
	PathParams  map[string]string
	Headers     map[string]string

	content     []byte
	contentType string
}

func newOpenAPIRequest() *openAPIRequest {
	return &openAPIRequest{
		QueryParams: map[string]string{},
		FormParams:  map[string]string{},
		PathParams:  map[string]string{},
		Headers:     map[string]string{},
	}
}

func (r *openAPIRequest) SetContent(content []byte) {
	r.content = append([]byte(nil), content...)
}

func (r *openAPIRequest) SetContentType(contentType string) {
	r.contentType = contentType
}

func (r *openAPIRequest) GetContent() []byte {
	return append([]byte(nil), r.content...)
}

func (r *openAPIRequest) GetBodyReader() io.Reader {
	return bytes.NewReader(r.content)
}

func (r *openAPIRequest) BodyValue() any {
	if len(r.content) == 0 {
		return nil
	}
	var decoded any
	if err := json.Unmarshal(r.content, &decoded); err == nil {
		return decoded
	}
	return string(r.content)
}

func (r *openAPIRequest) BuildPath() string {
	path := r.PathPattern
	if path == "" {
		return "/"
	}
	for key, value := range r.PathParams {
		path = strings.ReplaceAll(path, "["+key+"]", value)
		path = strings.ReplaceAll(path, "{"+key+"}", value)
	}
	return path
}

// TransToAcsRequest is kept as a no-op compatibility helper for tests that
// inspect the built path after the old request-conversion step.
func (r *openAPIRequest) TransToAcsRequest() {}

func (r *openAPIRequest) BuildQueries() string {
	return r.BuildPath()
}

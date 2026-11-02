package aliyun

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestQueryParameterEncodings(t *testing.T) {
	for _, tt := range []struct {
		key   string
		value any
		want  map[string]string
	}{
		{"Value", int64(7), map[string]string{"Value": "7"}},
		{"Value", float64(1.25), map[string]string{"Value": "1.25"}},
		{"Value", []string{"a", "b"}, map[string]string{"Value": "a,b"}},
		{"Value", nil, map[string]string{}},
		{"Value", "", map[string]string{}},
		{"Value", []string{}, map[string]string{}},
		{"ResourceGroupIds", []string{"rg-a", "", "rg-b"}, map[string]string{"ResourceGroupIds.1": "rg-a", "ResourceGroupIds.3": "rg-b"}},
		{"NodeIds", []any{"a", "b"}, map[string]string{"NodeIds": `["a","b"]`}},
		{"NodeIds", []any{"a", 1}, map[string]string{"NodeIds": "[a 1]"}},
		{"Value", []any{1, 2}, map[string]string{"Value": "[1 2]"}},
		{"Value", json.Number("42"), map[string]string{"Value": "42"}},
		{"Tag", []string{"env=prod"}, map[string]string{"Tag.1.Key": "env", "Tag.1.Value": "prod"}},
	} {
		got := map[string]string{}
		if err := setQueryParam(got, tt.key, tt.value); err != nil || !reflect.DeepEqual(got, tt.want) {
			t.Fatalf("encoding %s(%#v) = %#v, %v; want %#v", tt.key, tt.value, got, err, tt.want)
		}
	}
	if err := setQueryParam(map[string]string{}, "Tag", []string{"=bad"}); err == nil {
		t.Fatal("invalid tag accepted")
	}
	if got := stringMapAny(map[string]string{"key": "value"}); !reflect.DeepEqual(got, map[string]any{"key": "value"}) {
		t.Fatalf("map conversion = %#v", got)
	}
	if stringMapAny(nil) != nil || stringPointerMap(nil) != nil {
		t.Fatal("empty map conversion changed nil")
	}
}

func TestOpenAPIResponseShapeContract(t *testing.T) {
	for _, value := range []any{nil, " ", []any{}, []map[string]any{}, map[string]any{}} {
		if !isEmptyOpenAPIBody(value) {
			t.Fatalf("empty body considered populated: %#v", value)
		}
	}
	if isEmptyOpenAPIBody(1) {
		t.Fatal("numeric body considered empty")
	}
	for _, raw := range []string{`true`, `42`, `"text"`, `null`, `invalid`} {
		if _, err := decodeOpenAPIResponse(raw); err == nil {
			t.Fatalf("unsupported response accepted: %s", raw)
		}
	}
	for _, response := range []map[string]any{nil, {"body": nil}, {"body": ""}} {
		if got, err := normalizeDarabonbaResponse(response); err != nil || len(got) != 0 {
			t.Fatalf("empty response = %#v, %v", got, err)
		}
	}
	if _, err := normalizeDarabonbaResponse(map[string]any{"body": true}); err == nil {
		t.Fatal("boolean response accepted")
	}
	req := newOpenAPIRequest()
	req.SetContent([]byte("raw body"))
	if got := req.BodyValue(); got != "raw body" {
		t.Fatalf("raw body = %#v", got)
	}
	if req.BuildQueries() != "/" {
		t.Fatal("empty request path changed")
	}
	req.PathPattern = "/items/{id}/[version]"
	req.PathParams = map[string]string{"id": "item", "version": "v1"}
	req.TransToAcsRequest()
	if got := req.BuildQueries(); got != "/items/item/v1" {
		t.Fatalf("path = %s", got)
	}
	body := req.GetContent()
	body[0] = 'X'
	if strings.HasPrefix(string(req.GetContent()), "X") {
		t.Fatal("content leaked mutable backing array")
	}
}

func TestOpenAPIParameterMetadataControlsFlatteningAndErrors(t *testing.T) {
	for _, tt := range []struct {
		parameter OpenAPIParameter
		value     any
		want      map[string]string
	}{
		{OpenAPIParameter{Type: "RepeatList", ParamStyle: "simple"}, []int{1, 2}, map[string]string{"Value": "1,2"}},
		{OpenAPIParameter{Type: "RepeatList", ParamStyle: "flat", SubParameters: []OpenAPIParameter{{Name: "Name", Type: "String"}}}, []map[string]any{{"Name": "one"}}, map[string]string{"Value.1.Name": "one"}},
		{OpenAPIParameter{Type: "Struct", ParamStyle: "flat", Value: &OpenAPIParameter{Type: "String"}}, map[string]any{"Dynamic": "value"}, map[string]string{"Value.Dynamic": "value"}},
	} {
		out := map[string]string{}
		if err := setOpenAPIParamValue(out, &tt.parameter, "Value", tt.value); err != nil || !reflect.DeepEqual(out, tt.want) {
			t.Fatalf("metadata encoding=%#v, %v; want %#v", out, err, tt.want)
		}
	}
	for _, parameter := range []OpenAPIParameter{{Type: "Json"}, {Type: "RepeatList", ParamStyle: "flat", Element: &OpenAPIParameter{Type: "Json"}}, {Type: "Struct", ParamStyle: "flat", Value: &OpenAPIParameter{Type: "Json"}}} {
		var value any = make(chan int)
		if parameter.Type == "RepeatList" {
			value = []any{make(chan int)}
		}
		if parameter.Type == "Struct" {
			value = map[string]any{"Invalid": make(chan int)}
		}
		if err := setOpenAPIParamValue(map[string]string{}, &parameter, "Value", value); err == nil {
			t.Fatal("unserializable metadata value accepted")
		}
	}
	for _, values := range []map[string]any{{"body.": "bad"}, {"body.0": "bad"}, {"body.1": "one", "body.name": "mixed"}} {
		if _, err := flatOpenAPIBody(values, map[string]bool{}, "body", nil); err == nil {
			t.Fatalf("invalid body shape accepted: %#v", values)
		}
	}
	caller := &OpenAPICaller{Product: "ecs", Region: "cn-hangzhou"}
	if _, err := caller.commonRequest("DescribeInstances", map[string]any{"Tag": []string{"=bad"}}); err == nil {
		t.Fatal("invalid query tag accepted")
	}
}

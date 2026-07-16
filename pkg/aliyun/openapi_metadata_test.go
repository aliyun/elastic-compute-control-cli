package aliyun

import "testing"

func TestOpenAPIDetailFindParameterMatchesLegacyNestedParameter(t *testing.T) {
	detail := OpenAPIOperationDetail{
		Parameters: []OpenAPIParameter{
			{
				Name: "Container",
				SubParameters: []OpenAPIParameter{
					{Name: "Name", Position: "Query", Type: "String"},
				},
			},
			{Name: "Tags", Position: "Query", Type: "RepeatList"},
		},
	}

	param := detail.FindParameter("Container.1.Name")
	if param == nil || param.Name != "Name" {
		t.Fatalf("FindParameter nested = %#v, want Name", param)
	}
	param = detail.FindParameter("Tags.1")
	if param == nil || param.Name != "Tags" {
		t.Fatalf("FindParameter repeat list = %#v, want Tags", param)
	}
	if param := detail.FindParameter("Container.Name"); param != nil {
		t.Fatalf("FindParameter without repeat index = %#v, want nil", param)
	}
}

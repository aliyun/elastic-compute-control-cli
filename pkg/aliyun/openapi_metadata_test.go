package aliyun

import "testing"

func TestOpenAPINewMetadataReaders(t *testing.T) {
	product, ok := OpenAPIProductByCode("ecs", "en")
	if !ok {
		t.Fatal("OpenAPIProductByCode(ecs) did not find embedded metadata")
	}
	if product.Code != "Ecs" || product.Version != "2014-05-26" || product.Style != "rpc" {
		t.Fatalf("OpenAPIProductByCode(ecs) = %#v", product)
	}
	if operation, ok := OpenAPIOperationName(product, "describeinstances"); !ok || operation != "DescribeInstances" {
		t.Fatalf("OpenAPIOperationName(describeinstances) = %q, %v", operation, ok)
	}

	summary, ok := OpenAPIOperationSummaryFor("en", "ecs", "DescribeInstances")
	if !ok || summary.Title != "DescribeInstances" || summary.Summary == "" {
		t.Fatalf("OpenAPIOperationSummaryFor(DescribeInstances) = %#v, %v", summary, ok)
	}

	detail, ok := OpenAPIOperationDetailFor("en", product, "DescribeInstances")
	if !ok || detail.Name != "DescribeInstances" || detail.Method != "GET|POST" || len(detail.Parameters) == 0 {
		t.Fatalf("OpenAPIOperationDetailFor(DescribeInstances) = %#v, %v", detail, ok)
	}
}

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

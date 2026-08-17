package aliyun

import (
	"errors"
	"testing"
)

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

func TestOpenAPIOperationNamePrefersCurrentCanonicalCase(t *testing.T) {
	product := OpenAPIProduct{
		APINames:        []string{"CurrentOperation", "currentoperation"},
		currentMetadata: true,
		currentAPINames: map[string]bool{"CurrentOperation": true},
	}
	operation, ok := OpenAPIOperationName(product, "currentoperation")
	if !ok || operation != "CurrentOperation" {
		t.Fatalf("OpenAPIOperationName = %q, %v; want CurrentOperation, true", operation, ok)
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

func TestOpenAPIProductsStrictRejectsCurrentCatalogReadFailure(t *testing.T) {
	want := errors.New("broken current catalog")
	_, err := openAPIProductsStrictWithReader("en", func(language, path string) ([]byte, error) {
		return nil, want
	})
	if !errors.Is(err, want) {
		t.Fatalf("openAPIProductsStrictWithReader error = %v, want %v", err, want)
	}
}

func TestOpenAPIProductsStrictRejectsProductManifestFailure(t *testing.T) {
	want := errors.New("broken product manifest")
	_, err := openAPIProductsStrictWithReader("en", func(language, path string) ([]byte, error) {
		switch path {
		case "/products.json":
			return []byte(`{"products":[{"code":"Broken"}]}`), nil
		case "/broken/version.json":
			return nil, want
		default:
			t.Fatalf("unexpected metadata path %q", path)
			return nil, nil
		}
	}, "Broken")
	if !errors.Is(err, want) {
		t.Fatalf("openAPIProductsStrictWithReader error = %v, want %v", err, want)
	}
}

func TestOpenAPIProductsStrictRejectsProductManifestWithoutAPIs(t *testing.T) {
	_, err := openAPIProductsStrictWithReader("en", func(language, path string) ([]byte, error) {
		switch path {
		case "/products.json":
			return []byte(`{"products":[{"code":"Broken"}]}`), nil
		case "/broken/version.json":
			return []byte(`{"version":"2020-01-01"}`), nil
		default:
			t.Fatalf("unexpected metadata path %q", path)
			return nil, nil
		}
	}, "Broken")
	if err == nil {
		t.Fatal("product manifest without APIs unexpectedly succeeded")
	}
}

func TestOpenAPIProductsStrictValidatesOnlyRequiredProductManifests(t *testing.T) {
	products, err := openAPIProductsStrictWithReader("en", func(language, path string) ([]byte, error) {
		switch path {
		case "/products.json":
			return []byte(`{"products":[{"code":"Required"},{"code":"Unrelated"}]}`), nil
		case "/required/version.json":
			return []byte(`{"apis":{"DoThing":{}}}`), nil
		case "/unrelated/version.json":
			t.Fatal("strict loader read an unrelated product manifest")
			return nil, nil
		default:
			t.Fatalf("unexpected metadata path %q", path)
			return nil, nil
		}
	}, "Required")
	if err != nil {
		t.Fatalf("openAPIProductsStrictWithReader: %v", err)
	}
	found := false
	for _, product := range products {
		if product.Code == "Required" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("strict loader omitted required product")
	}
}

func TestOpenAPIOperationDetailStrictRejectsBrokenCurrentDetail(t *testing.T) {
	tests := []struct {
		name   string
		detail func() ([]byte, error)
	}{
		{
			name: "read failure",
			detail: func() ([]byte, error) {
				return nil, errors.New("missing current detail")
			},
		},
		{
			name: "malformed JSON",
			detail: func() ([]byte, error) {
				return []byte(`{"name":`), nil
			},
		},
		{
			name: "empty detail",
			detail: func() ([]byte, error) {
				return []byte(`{}`), nil
			},
		},
		{
			name: "mismatched detail",
			detail: func() ([]byte, error) {
				return []byte(`{"name":"Other"}`), nil
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			legacyCalled := false
			_, err := openAPIOperationDetailForStrictWithReaders(
				"en",
				OpenAPIProduct{
					Code:            "Test",
					APINames:        []string{"Current"},
					currentMetadata: true,
					currentAPINames: map[string]bool{"Current": true},
				},
				"Current",
				func(language, path string) ([]byte, error) {
					switch path {
					case "/test/Current.json":
						return tt.detail()
					default:
						t.Fatalf("unexpected metadata path %q", path)
						return nil, nil
					}
				},
				func(string, OpenAPIProduct, string) (OpenAPIOperationDetail, bool) {
					legacyCalled = true
					return OpenAPIOperationDetail{Name: "legacy"}, true
				},
			)
			if err == nil {
				t.Fatal("strict current detail unexpectedly succeeded")
			}
			if legacyCalled {
				t.Fatal("strict current detail fell back to legacy metadata")
			}
		})
	}
}

func TestOpenAPIOperationDetailStrictAllowsExplicitLegacyOnlyOperation(t *testing.T) {
	want := OpenAPIOperationDetail{Name: "LegacyOnly"}
	got, err := openAPIOperationDetailForStrictWithReaders(
		"en",
		OpenAPIProduct{
			Code:            "Test",
			APINames:        []string{"LegacyOnly"},
			currentMetadata: true,
			currentAPINames: map[string]bool{},
		},
		"LegacyOnly",
		func(language, path string) ([]byte, error) {
			t.Fatalf("legacy-only operation unexpectedly read current detail %q", path)
			return nil, nil
		},
		func(string, OpenAPIProduct, string) (OpenAPIOperationDetail, bool) {
			return want, true
		},
	)
	if err != nil {
		t.Fatalf("openAPIOperationDetailForStrictWithReaders: %v", err)
	}
	if got.Name != want.Name {
		t.Fatalf("strict legacy-only detail = %#v, want %#v", got, want)
	}
}

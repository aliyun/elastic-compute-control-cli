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

func TestOpenAPIMetadataResolverRejectsCurrentCatalogReadFailure(t *testing.T) {
	want := errors.New("broken current catalog")
	_, err := newOpenAPIMetadataResolverWithReader("en", []string{"ecs"}, func(language, path string) ([]byte, error) {
		return nil, want
	})
	if !errors.Is(err, want) {
		t.Fatalf("newOpenAPIMetadataResolverWithReader error = %v, want %v", err, want)
	}
}

func TestOpenAPIMetadataResolverRejectsProductManifestFailure(t *testing.T) {
	want := errors.New("broken product manifest")
	_, err := newOpenAPIMetadataResolverWithReader("en", []string{"Broken"}, func(language, path string) ([]byte, error) {
		switch path {
		case "/products.json":
			return []byte(`{"products":[{"code":"Broken"}]}`), nil
		case "/broken/version.json":
			return nil, want
		default:
			t.Fatalf("unexpected metadata path %q", path)
			return nil, nil
		}
	})
	if !errors.Is(err, want) {
		t.Fatalf("newOpenAPIMetadataResolverWithReader error = %v, want %v", err, want)
	}
}

func TestOpenAPIMetadataResolverRejectsProductManifestWithoutAPIs(t *testing.T) {
	_, err := newOpenAPIMetadataResolverWithReader("en", []string{"Broken"}, func(language, path string) ([]byte, error) {
		switch path {
		case "/products.json":
			return []byte(`{"products":[{"code":"Broken"}]}`), nil
		case "/broken/version.json":
			return []byte(`{"version":"2020-01-01"}`), nil
		default:
			t.Fatalf("unexpected metadata path %q", path)
			return nil, nil
		}
	})
	if err == nil {
		t.Fatal("product manifest without APIs unexpectedly succeeded")
	}
}

func TestOpenAPIMetadataResolverRejectsRequiredProductMissingFromCurrentCatalog(t *testing.T) {
	_, err := newOpenAPIMetadataResolverWithReader("en", []string{"Ecs"}, func(language, path string) ([]byte, error) {
		if path != "/products.json" {
			t.Fatalf("unexpected metadata path %q", path)
		}
		return []byte(`{"products":[{"code":"Other"}]}`), nil
	})
	if err == nil {
		t.Fatal("required product missing from current catalog unexpectedly succeeded")
	}
}

func TestOpenAPIMetadataResolverValidatesOnlyRequiredProductManifests(t *testing.T) {
	resolver, err := newOpenAPIMetadataResolverWithReader("en", []string{"Required"}, func(language, path string) ([]byte, error) {
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
	})
	if err != nil {
		t.Fatalf("newOpenAPIMetadataResolverWithReader: %v", err)
	}
	if resolver == nil {
		t.Fatal("strict loader returned a nil resolver")
	}
}

func TestOpenAPIMetadataResolverRequiresExplicitLegacyOperationApproval(t *testing.T) {
	resolver, err := NewOpenAPIMetadataResolver("en", []string{"ecs"})
	if err != nil {
		t.Fatalf("NewOpenAPIMetadataResolver: %v", err)
	}
	if _, _, err := resolver.OperationLeaves("ecs", "CloneDisks", false); err == nil {
		t.Fatal("legacy-only ecs.CloneDisks succeeded without approval")
	}
	leaves, operation, err := resolver.OperationLeaves("ecs", "CloneDisks", true)
	if err != nil {
		t.Fatalf("approved legacy-only ecs.CloneDisks: %v", err)
	}
	if operation != "CloneDisks" || len(leaves) == 0 {
		t.Fatalf("approved legacy-only operation = %q with %d leaves", operation, len(leaves))
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
			name: "missing parameters",
			detail: func() ([]byte, error) {
				return []byte(`{"name":"Current"}`), nil
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
				false,
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
		true,
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

func TestOpenAPIOperationDetailStrictRejectsUnapprovedLegacyFallback(t *testing.T) {
	_, err := openAPIOperationDetailForStrictWithReaders(
		"en",
		OpenAPIProduct{
			Code:            "Test",
			APINames:        []string{"LegacyOnly"},
			currentMetadata: true,
			currentAPINames: map[string]bool{},
		},
		"LegacyOnly",
		false,
		func(language, path string) ([]byte, error) {
			t.Fatalf("unapproved legacy operation unexpectedly read current detail %q", path)
			return nil, nil
		},
		func(string, OpenAPIProduct, string) (OpenAPIOperationDetail, bool) {
			return OpenAPIOperationDetail{Name: "LegacyOnly"}, true
		},
	)
	if err == nil {
		t.Fatal("unapproved legacy operation unexpectedly succeeded")
	}
}

func TestOpenAPIOperationDetailStrictAllowsExplicitEmptyParameterList(t *testing.T) {
	detail, err := openAPIOperationDetailForStrictWithReaders(
		"en",
		OpenAPIProduct{
			Code:            "Test",
			APINames:        []string{"NoParameters"},
			currentMetadata: true,
			currentAPINames: map[string]bool{"NoParameters": true},
		},
		"NoParameters",
		false,
		func(language, path string) ([]byte, error) {
			return []byte(`{"name":"NoParameters","parameters":[]}`), nil
		},
		func(string, OpenAPIProduct, string) (OpenAPIOperationDetail, bool) {
			t.Fatal("current operation unexpectedly fell back to legacy")
			return OpenAPIOperationDetail{}, false
		},
	)
	if err != nil {
		t.Fatalf("explicit empty parameter list failed: %v", err)
	}
	if detail.Parameters == nil || len(detail.Parameters) != 0 {
		t.Fatalf("explicit empty parameters = %#v, want non-nil empty slice", detail.Parameters)
	}
}

func TestOpenAPIOperationDetailFromNewMetaPreservesSubParameters(t *testing.T) {
	detail := openAPIOperationDetailFromNewMeta(OpenAPIProduct{}, &openAPINewDetail{
		Name: "Nested",
		Parameters: []openAPINewRequestParameter{{
			Name: "Group",
			Type: "RepeatList",
			SubParameters: []openAPINewRequestParameter{{
				Name: "CurrentOnlyChild",
				Type: "String",
			}},
		}},
	})
	if len(detail.Parameters) != 1 || len(detail.Parameters[0].SubParameters) != 1 ||
		detail.Parameters[0].SubParameters[0].Name != "CurrentOnlyChild" {
		t.Fatalf("converted nested parameters = %#v", detail.Parameters)
	}
}

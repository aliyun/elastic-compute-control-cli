package aliyun

import (
	"testing"
)

func leafNames(leaves []OpenAPIParameter) map[string]bool {
	names := map[string]bool{}
	for _, leaf := range leaves {
		names[leaf.Name] = true
	}
	return names
}

func requireLeaf(t *testing.T, names map[string]bool, name string) {
	t.Helper()
	if !names[name] {
		t.Errorf("OpenAPIOperationLeaves missing leaf %q", name)
	}
}

func requireNoLeaf(t *testing.T, names map[string]bool, name string) {
	t.Helper()
	if names[name] {
		t.Errorf("OpenAPIOperationLeaves produced unexpected bare group leaf %q", name)
	}
}

func TestOpenAPIOperationLeavesRunInstances(t *testing.T) {
	product, ok := OpenAPIProductByCode("ecs", "en")
	if !ok {
		t.Fatal("OpenAPIProductByCode(ecs) failed")
	}
	leaves, ok := OpenAPIOperationLeaves("en", product, "RunInstances")
	if !ok {
		t.Fatal("OpenAPIOperationLeaves(RunInstances) failed")
	}
	names := leafNames(leaves)

	for _, want := range []string{
		// Groups are expanded exclusively from canonical fields/element.fields.
		"DataDisk.Category", "DataDisk.Size", "DataDisk.BurstingEnabled",
		"NetworkInterface.VSwitchId", "NetworkInterface.NetworkCardIndex",
		"Arn.AssumeRoleFor", "Arn.RoleType", "Arn.Rolearn",
		"Tag.Key", "Tag.Value",
		// Dotted flat leaves survive.
		"CpuOptions.Core", "CpuOptions.Numa", "SystemDisk.Category",
		"SystemDisk.Size", "PrivatePoolOptions.Id", "SecurityOptions.TrustedSystemMode",
		// Canonical now supplies children for these formerly opaque groups.
		"ClockOptions.PtpStatus", "ImageOptions.LoginAsNonRoot", "NetworkOptions.EnableJumboFrame", "PrivateDnsNameOptions.HostnameType",
		// Scalar RepeatList parameters without children stay as leaves.
		"Ipv6Address", "HostNames", "SecurityGroupIds",
		// Plain scalars.
		"ZoneId", "InstanceType", "UserData", "ClientToken", "RegionId",
	} {
		requireLeaf(t, names, want)
	}

	for _, absent := range []string{
		"DataDisk", "SystemDisk", "Arn", "Tag", "NetworkInterface", "CpuOptions",
	} {
		requireNoLeaf(t, names, absent)
	}
}

func TestOpenAPIOperationLeavesPrefersCurrentMetadata(t *testing.T) {
	product, ok := OpenAPIProductByCode("ecs", "en")
	if !ok {
		t.Fatal("OpenAPIProductByCode(ecs) failed")
	}
	leaves, ok := OpenAPIOperationLeaves("en", product, "RunInstances")
	if !ok {
		t.Fatal("OpenAPIOperationLeaves(RunInstances) failed")
	}
	for _, leaf := range leaves {
		if leaf.Name == "Affinity" {
			if leaf.Description == "" {
				t.Fatal("Affinity description is empty; legacy metadata was selected instead of current metadata")
			}
			return
		}
	}
	t.Fatal("RunInstances Affinity parameter missing")
}

func TestOpenAPIOperationLeavesStrictUsesCurrentMetadata(t *testing.T) {
	resolver, err := NewOpenAPIMetadataResolver("en", []string{"ecs"})
	if err != nil {
		t.Fatalf("NewOpenAPIMetadataResolver: %v", err)
	}
	leaves, operation, err := resolver.OperationLeaves("ecs", "RunInstances", false)
	if err != nil {
		t.Fatalf("OperationLeaves(RunInstances): %v", err)
	}
	if operation != "RunInstances" {
		t.Fatalf("OperationLeaves operation = %q, want RunInstances", operation)
	}
	names := leafNames(leaves)
	requireLeaf(t, names, "ZoneId")
	requireLeaf(t, names, "DataDisk.Category")
	requireNoLeaf(t, names, "DataDisk")
}

func TestCanonicalLeavesDoNotRestoreMissingGroupChildren(t *testing.T) {
	product, ok := OpenAPIProductByCode("ecs", "en")
	if !ok {
		t.Fatal("Ecs metadata missing")
	}
	resolver := &OpenAPIMetadataResolver{
		language: "en", products: map[string]OpenAPIProduct{"ecs": product},
		read: func(language, path string) ([]byte, error) {
			return []byte(`{"name":"RunInstances","parameters":[{"name":"DataDisk","type":"RepeatList"},{"name":"CurrentOnly","type":"String"}]}`), nil
		},
	}
	leaves, _, err := resolver.OperationLeaves("ecs", "RunInstances", false)
	if err != nil {
		t.Fatal(err)
	}
	assertLeafNames(t, leaves, []string{"CurrentOnly", "DataDisk"})
}

func TestFlattenCurrentDottedChildrenAreAuthoritative(t *testing.T) {
	detail := OpenAPIOperationDetail{Parameters: []OpenAPIParameter{
		{Name: "Group", Type: "Struct"},
		{Name: "Group.CurrentChild", Type: "String"},
	}}
	assertLeafNames(t, flattenOpenAPIParameters(detail), []string{"Group.CurrentChild"})
}

func assertLeafNames(t *testing.T, leaves []OpenAPIParameter, want []string) {
	t.Helper()
	got := make(map[string]bool, len(leaves))
	for _, leaf := range leaves {
		got[leaf.Name] = true
	}
	if len(got) != len(want) {
		t.Fatalf("leaf names = %#v, want %v", got, want)
	}
	for _, name := range want {
		if !got[name] {
			t.Errorf("leaf %q missing from %#v", name, got)
		}
	}
}

func TestFlattenOpenAPIParametersFallbackShape(t *testing.T) {
	// The newer metadata snapshot can lack sub-parameters: groups appear as
	// bare placeholders and struct children as dotted flat leaves.
	detail := OpenAPIOperationDetail{
		Parameters: []OpenAPIParameter{
			{Name: "ZoneId", Type: "String"},
			{Name: "DataDisk", Type: "RepeatList"},
			{Name: "SystemDisk", Type: "Struct"},
			{Name: "SystemDisk.Category", Type: "String"},
			{Name: "ClockOptions", Type: "Struct"},
		},
	}
	leaves := flattenOpenAPIParameters(detail)
	names := leafNames(leaves)

	requireLeaf(t, names, "ZoneId")
	// A bare RepeatList with no child information contributes itself.
	requireLeaf(t, names, "DataDisk")
	// The bare Struct parent is dropped when dotted children exist.
	requireNoLeaf(t, names, "SystemDisk")
	requireLeaf(t, names, "SystemDisk.Category")
	// An opaque Struct with no child information contributes itself.
	requireLeaf(t, names, "ClockOptions")
}

func TestFlattenOpenAPIParametersMultiLevelAncestors(t *testing.T) {
	// A bare a Struct placeholder whose descendants are declared as a dotted
	// flat name several levels deep must be suppressed at every ancestor level,
	// not just the immediate parent.
	detail := OpenAPIOperationDetail{
		Parameters: []OpenAPIParameter{
			{Name: "a", Type: "Struct"},
			{Name: "a.b.c", Type: "String"},
		},
	}
	leaves := flattenOpenAPIParameters(detail)
	names := leafNames(leaves)
	requireNoLeaf(t, names, "a")
	requireNoLeaf(t, names, "a.b")
	requireLeaf(t, names, "a.b.c")
}

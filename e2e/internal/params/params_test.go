package params

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveECSPropagatesFatalQueryError(t *testing.T) {
	fatal := MarkFatalQueryError(errors.New("access denied"))
	query := func(context.Context, string) (any, error) { return nil, fatal }
	_, err := ResolveECS(context.Background(), ECSPolicy{Cores: []int{1}, ImageFamily: "family"}, query, "cn-hangzhou", "")
	if err == nil || !IsFatalQueryError(err) || !errors.Is(err, fatal) {
		t.Fatalf("err = %v, want original fatal query error", err)
	}
}

func TestResolveECSPropagatesUnexpectedQueryError(t *testing.T) {
	queryErr := errors.New("internal service error")
	query := func(context.Context, string) (any, error) { return nil, queryErr }
	_, err := ResolveECS(context.Background(), ECSPolicy{Cores: []int{1}, ImageFamily: "family"}, query, "cn-hangzhou", "")
	if !errors.Is(err, queryErr) {
		t.Fatalf("err = %v, want unexpected query error", err)
	}
}

func TestCandidateUnavailableMarkersStayNarrow(t *testing.T) {
	for _, text := range []string{"InvalidRegionId", "Forbidden.Region", "InsufficientStock", "NoStock"} {
		if !IsCandidateUnavailableText(text) {
			t.Fatalf("%q should identify a candidate-unavailable error", text)
		}
	}
	for _, text := range []string{"OperationNotSupported", "InternalError", "QuotaExceeded", "AccessDenied"} {
		if IsCandidateUnavailableText(text) {
			t.Fatalf("%q must not trigger region fallback", text)
		}
	}
}

func TestLoadPolicyRejectsUnknownFieldsAndValidatesImageSelectorOnDemand(t *testing.T) {
	unknown := filepath.Join(t.TempDir(), "unknown.yaml")
	if err := os.WriteFile(unknown, []byte("ecs:\n  cores: [1]\n  image_family: family\n  image_owner_alias: system\n  typo: value\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadPolicy(unknown); err == nil || !strings.Contains(err.Error(), "field typo") {
		t.Fatalf("unknown-field error = %v", err)
	}

	missing := filepath.Join(t.TempDir(), "missing-image.yaml")
	if err := os.WriteFile(missing, []byte("ecs:\n  cores: [1]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	policy, err := LoadPolicy(missing)
	if err != nil {
		t.Fatalf("structural load: %v", err)
	}
	if err := policy.ValidateFor([]string{"ecs.zone"}); err != nil {
		t.Fatalf("zone-only policy validation: %v", err)
	}
	if err := policy.ValidateFor([]string{"ecs.image_id"}); err == nil || !strings.Contains(err.Error(), "image_family") {
		t.Fatalf("missing-image validation error = %v", err)
	}
}

func TestLoadPolicyRejectsInvalidCoreList(t *testing.T) {
	for name, cores := range map[string]string{
		"zero":      "[0]",
		"negative":  "[-1]",
		"duplicate": "[1, 1]",
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "parameter-policy.yaml")
			content := fmt.Sprintf("ecs:\n  cores: %s\n  image_family: family\n", cores)
			if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadPolicy(path); err == nil {
				t.Fatalf("cores %s should be rejected", cores)
			}
		})
	}
}

func TestPolicyRejectsEmptyCandidateSetsOnlyWhenRequired(t *testing.T) {
	path := filepath.Join(t.TempDir(), "parameter-policy.yaml")
	if err := os.WriteFile(path, []byte("ecs: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	policy, err := LoadPolicy(path)
	if err != nil {
		t.Fatalf("structural load: %v", err)
	}
	if err := policy.ValidateFor([]string{"ack.cluster_type"}); err != nil {
		t.Fatalf("ACK metadata must not require ECS candidates: %v", err)
	}
	if err := policy.ValidateFor([]string{"ecs.instance_type"}); err == nil {
		t.Fatal("expected instance-type candidate validation error")
	}
}

func TestResolveECSForZoneUsesOnlyZoneInventory(t *testing.T) {
	var commands []string
	query := func(_ context.Context, command string) (any, error) {
		commands = append(commands, command)
		if command != "ecctl ecs zone list --verbose" {
			return nil, fmt.Errorf("unexpected query %q", command)
		}
		return map[string]any{"zones": []any{
			map[string]any{"id": "cn-hangzhou-a", "available_resource_creation": []any{"Instance"}},
			map[string]any{"id": "cn-hangzhou-a-alipay", "available_resource_creation": []any{"VSwitch", "Instance"}},
			map[string]any{"id": "cn-hangzhou-c", "available_resource_creation": []any{"VSwitch", "Instance"}},
			map[string]any{"id": "cn-hangzhou-b", "available_resource_creation": []any{"VSwitch", "Instance"}},
		}}, nil
	}

	got, err := ResolveECSFor(context.Background(), ECSPolicy{}, query, "cn-hangzhou", "", []string{"ecs.zone"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Zone != "cn-hangzhou-c" {
		t.Fatalf("zone = %q, want first compatible inventory entry cn-hangzhou-c", got.Zone)
	}
	if len(commands) != 1 {
		t.Fatalf("commands = %v, want zone inventory only", commands)
	}
}

func TestResolveECSForQueriesOnlyRequestedDimensions(t *testing.T) {
	tests := []struct {
		name      string
		required  string
		wantCalls []string
		forbidden []string
	}{
		{name: "instance", required: "ecs.instance_type", wantCalls: []string{"DestinationResource InstanceType"}, forbidden: []string{"SystemDisk", "DataDisk", "DescribeImages"}},
		{name: "image", required: "ecs.image_id", wantCalls: []string{"DestinationResource InstanceType", "DescribeImages"}, forbidden: []string{"SystemDisk", "DataDisk"}},
		{name: "system disk", required: "ecs.system_disk_category", wantCalls: []string{"DestinationResource InstanceType", "DestinationResource SystemDisk"}, forbidden: []string{"DestinationResource DataDisk", "DescribeImages"}},
		{name: "data disk", required: "ecs.data_disk_category", wantCalls: []string{"DestinationResource InstanceType", "DestinationResource SystemDisk", "DestinationResource DataDisk"}, forbidden: []string{"DescribeImages"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var commands []string
			query := func(_ context.Context, command string) (any, error) {
				commands = append(commands, command)
				switch {
				case strings.Contains(command, "DestinationResource InstanceType"):
					return stockedInstanceResponse("cn-hangzhou-b", "ecs.g7.large"), nil
				case strings.Contains(command, "DestinationResource SystemDisk"):
					return diskResponse("SystemDisk", "cloud_efficiency"), nil
				case strings.Contains(command, "DestinationResource DataDisk"):
					return diskResponse("DataDisk", "cloud_efficiency"), nil
				case strings.Contains(command, "DescribeImages"):
					return map[string]any{"response": map[string]any{"Images": map[string]any{"Image": []any{map[string]any{"ImageId": "img-1"}}}}}, nil
				default:
					return nil, fmt.Errorf("unexpected query %q", command)
				}
			}
			_, err := ResolveECSFor(context.Background(), ECSPolicy{Cores: []int{1}, ImageFamily: "family"}, query, "cn-hangzhou", "", []string{tt.required})
			if err != nil {
				t.Fatal(err)
			}
			joined := strings.Join(commands, "\n")
			for _, wanted := range tt.wantCalls {
				if !strings.Contains(joined, wanted) {
					t.Fatalf("commands = %v, want %q", commands, wanted)
				}
			}
			for _, forbidden := range tt.forbidden {
				if strings.Contains(joined, forbidden) {
					t.Fatalf("commands = %v, unrequested dimension %q was queried", commands, forbidden)
				}
			}
		})
	}
}

func TestResolveECSUsesCoreOrderAndDynamicallySelectsDisks(t *testing.T) {
	var commands []string
	query := func(_ context.Context, command string) (any, error) {
		commands = append(commands, command)
		switch {
		case strings.Contains(command, "DestinationResource InstanceType") && strings.Contains(command, "--Cores 1"):
			return availabilityResponse(), nil
		case strings.Contains(command, "DestinationResource InstanceType") && strings.Contains(command, "--Cores 2"):
			return map[string]any{
				"response": map[string]any{
					"AvailableZones": map[string]any{"AvailableZone": []any{
						map[string]any{"ZoneId": "cn-hangzhou-b", "Status": "Available", "StatusCategory": "WithStock", "AvailableResources": map[string]any{"AvailableResource": []any{
							map[string]any{"Type": "InstanceType", "SupportedResources": map[string]any{"SupportedResource": []any{
								map[string]any{"Value": "ecs.g7.large", "Status": "Available", "StatusCategory": "WithStock"},
								map[string]any{"Value": "ecs.g6.large", "Status": "Available", "StatusCategory": "WithStock"},
							}}},
						}}},
					}},
				},
			}, nil
		case strings.Contains(command, "DestinationResource SystemDisk"):
			return diskResponse("SystemDisk", "cloud_ssd", "cloud_efficiency"), nil
		case strings.Contains(command, "DestinationResource DataDisk"):
			return diskResponse("DataDisk", "cloud_ssd", "cloud_efficiency"), nil
		case strings.Contains(command, "DescribeImages"):
			return map[string]any{
				"response": map[string]any{
					"Images": map[string]any{"Image": []any{map[string]any{"ImageId": "aliyun_3_x64"}}},
				},
			}, nil
		default:
			return nil, fmt.Errorf("unexpected query %q", command)
		}
	}

	got, err := ResolveECS(context.Background(), ECSPolicy{
		Cores:           []int{1, 2, 4},
		ImageFamily:     "acs:alibaba_cloud_linux_3_2104_lts_x64",
		ImageOwnerAlias: "system",
		IOOptimized:     "optimized",
	}, query, "cn-hangzhou", "")
	if err != nil {
		t.Fatal(err)
	}
	if got.InstanceType != "ecs.g6.large" || got.Zone != "cn-hangzhou-b" || got.ImageID != "aliyun_3_x64" || got.SystemDiskCategory != "cloud_efficiency" || got.DataDiskCategory != "cloud_efficiency" {
		t.Fatalf("result = %+v", got)
	}
	joined := strings.Join(commands, "\n")
	if !strings.Contains(joined, "--InstanceChargeType PostPaid") || !strings.Contains(joined, "--DestinationResource InstanceType") || !strings.Contains(joined, "--IoOptimized optimized") || !strings.Contains(joined, "--Cores 1") || !strings.Contains(joined, "--Cores 2") || strings.Contains(joined, "--Cores 4") {
		t.Fatalf("core discovery sequence = %v", commands)
	}
	if !strings.Contains(joined, "--DestinationResource SystemDisk") || !strings.Contains(joined, "--DestinationResource DataDisk") || !strings.Contains(joined, "--SystemDiskCategory cloud_efficiency") {
		t.Fatalf("disk availability was not verified: %v", commands)
	}
}

func TestResolveECSForWithConstraintsFiltersInstanceENICapacity(t *testing.T) {
	var commands []string
	query := func(_ context.Context, command string) (any, error) {
		commands = append(commands, command)
		switch {
		case strings.Contains(command, "DestinationResource InstanceType"):
			return stockedInstanceResponse("cn-hangzhou-b", "ecs.a-small-eni", "ecs.b-large-eni"), nil
		case strings.Contains(command, "DescribeInstanceTypes") && strings.Contains(command, "ecs.a-small-eni"):
			return map[string]any{"response": map[string]any{"InstanceTypes": map[string]any{"InstanceType": []any{}}}}, nil
		case strings.Contains(command, "DescribeInstanceTypes") && strings.Contains(command, "ecs.b-large-eni"):
			return map[string]any{"response": map[string]any{"InstanceTypes": map[string]any{"InstanceType": []any{
				map[string]any{"InstanceTypeId": "ecs.b-large-eni", "EniQuantity": 4},
			}}}}, nil
		default:
			return nil, fmt.Errorf("unexpected query %q", command)
		}
	}

	got, err := ResolveECSForWithConstraints(context.Background(), ECSPolicy{Cores: []int{1}}, query, "cn-hangzhou", "",
		[]string{"ecs.instance_type"}, ECSConstraints{MinENIQuantity: 2, MinENIPrivateIPAddressQuantity: 6})
	if err != nil {
		t.Fatal(err)
	}
	if got.InstanceType != "ecs.b-large-eni" {
		t.Fatalf("instance type = %q, want ENI-capable candidate", got.InstanceType)
	}
	joined := strings.Join(commands, "\n")
	if !strings.Contains(joined, "--MinimumEniQuantity 2") || !strings.Contains(joined, "--MinimumEniPrivateIpAddressQuantity 6") || !strings.Contains(joined, "--InstanceTypes.1 ecs.a-small-eni") {
		t.Fatalf("capability queries = %v", commands)
	}
}

func TestResolveECSForWithConstraintsFiltersSystemDiskCategories(t *testing.T) {
	query := func(_ context.Context, command string) (any, error) {
		switch {
		case strings.Contains(command, "DestinationResource InstanceType"):
			return stockedInstanceResponse("cn-hangzhou-b", "ecs.g7.large"), nil
		case strings.Contains(command, "DestinationResource SystemDisk"):
			return diskResponse("SystemDisk", "cloud_efficiency", "cloud_essd"), nil
		default:
			return nil, fmt.Errorf("unexpected query %q", command)
		}
	}

	got, err := ResolveECSForWithConstraints(context.Background(), ECSPolicy{Cores: []int{1}}, query, "cn-hangzhou", "",
		[]string{"ecs.instance_type", "ecs.system_disk_category"}, ECSConstraints{AllowedSystemDiskCategories: []string{"cloud_essd", "cloud_auto"}})
	if err != nil {
		t.Fatal(err)
	}
	if got.SystemDiskCategory != "cloud_essd" {
		t.Fatalf("system disk category = %q, want constrained ESSD category", got.SystemDiskCategory)
	}
}

func TestResolveECSForWithConstraintsFiltersDataDiskCategories(t *testing.T) {
	query := func(_ context.Context, command string) (any, error) {
		switch {
		case strings.Contains(command, "DestinationResource InstanceType"):
			return stockedInstanceResponse("cn-hangzhou-b", "ecs.g7.large"), nil
		case strings.Contains(command, "DestinationResource SystemDisk"):
			return diskResponse("SystemDisk", "cloud_efficiency"), nil
		case strings.Contains(command, "DestinationResource DataDisk"):
			return diskResponse("DataDisk", "cloud_efficiency", "cloud_essd"), nil
		default:
			return nil, fmt.Errorf("unexpected query %q", command)
		}
	}

	got, err := ResolveECSForWithConstraints(context.Background(), ECSPolicy{Cores: []int{1}}, query, "cn-hangzhou", "",
		[]string{"ecs.instance_type", "ecs.data_disk_category"}, ECSConstraints{AllowedDataDiskCategories: []string{"cloud_essd", "cloud_auto"}})
	if err != nil {
		t.Fatal(err)
	}
	if got.DataDiskCategory != "cloud_essd" {
		t.Fatalf("data disk category = %q, want constrained ESSD category", got.DataDiskCategory)
	}
}

func TestResolveECSRejectsExplicitUnavailableZone(t *testing.T) {
	query := func(_ context.Context, command string) (any, error) {
		if strings.Contains(command, "DestinationResource InstanceType") {
			return stockedInstanceResponse("cn-hangzhou-b", "ecs.g7.large"), nil
		}
		return nil, fmt.Errorf("unexpected query %q", command)
	}
	_, err := ResolveECS(context.Background(), ECSPolicy{Cores: []int{1}, ImageFamily: "family"}, query, "cn-hangzhou", "cn-hangzhou-a")
	if err == nil || !strings.Contains(err.Error(), "cn-hangzhou-a") {
		t.Fatalf("err = %v, want explicit-zone failure", err)
	}
}

func availabilityResponse() map[string]any {
	return map[string]any{"response": map[string]any{"AvailableZones": map[string]any{"AvailableZone": []any{}}}}
}

func stockedInstanceResponse(zone string, instanceTypes ...string) map[string]any {
	resources := make([]any, 0, len(instanceTypes))
	for _, instanceType := range instanceTypes {
		resources = append(resources, map[string]any{"Value": instanceType, "Status": "Available", "StatusCategory": "WithStock"})
	}
	return map[string]any{"response": map[string]any{"AvailableZones": map[string]any{"AvailableZone": []any{
		map[string]any{"ZoneId": zone, "Status": "Available", "StatusCategory": "WithStock", "AvailableResources": map[string]any{"AvailableResource": []any{
			map[string]any{"Type": "InstanceType", "SupportedResources": map[string]any{"SupportedResource": resources}},
		}}},
	}}}}
}

func diskResponse(resourceType string, categories ...string) map[string]any {
	resources := make([]any, 0, len(categories))
	for _, category := range categories {
		resources = append(resources, map[string]any{"Value": category, "Status": "Available"})
	}
	return map[string]any{"response": map[string]any{"AvailableZones": map[string]any{"AvailableZone": []any{
		map[string]any{"ZoneId": "cn-hangzhou-b", "Status": "Available", "StatusCategory": "WithStock", "AvailableResources": map[string]any{"AvailableResource": []any{
			map[string]any{"Type": resourceType, "SupportedResources": map[string]any{"SupportedResource": resources}},
		}}},
	}}}}
}

func TestResolveACKSelectsCreatableClusterTypeAndVersion(t *testing.T) {
	var commands []string
	query := func(_ context.Context, command string) (any, error) {
		commands = append(commands, command)
		switch {
		case strings.Contains(command, "--cluster-type Kubernetes"):
			return nil, errors.New("InvalidParameter.ClusterType: unsupported")
		case strings.Contains(command, "--cluster-type ManagedKubernetes"):
			return map[string]any{"versions": []any{
				map[string]any{
					"version": "1.31.1-aliyun.1", "creatable": true, "edition": "ack.standard",
					"upgradable_versions": []any{"1.32.0-aliyun.1"},
					"runtimes":            []any{map[string]any{"name": "containerd", "version": "1.6.28"}},
				},
			}}, nil
		default:
			return nil, fmt.Errorf("unexpected query %q", command)
		}
	}
	got, err := ResolveACK(context.Background(), query, "cn-hangzhou", []string{"Kubernetes", "ManagedKubernetes"}, []string{"ack.version", "ack.upgrade_version", "ack.runtime", "ack.runtime_version"}, ECSResult{Zone: "cn-hangzhou-b", InstanceType: "ecs.g7.large", ImageID: "img-1", SystemDiskCategory: "cloud_essd"})
	if err != nil {
		t.Fatal(err)
	}
	if got.ClusterType != "ManagedKubernetes" || got.Version != "1.31.1-aliyun.1" || got.UpgradeVersion != "1.32.0-aliyun.1" || got.Edition != "ack.standard" || got.Profile != "Default" || got.Runtime != "containerd" || got.RuntimeVersion != "1.6.28" || got.Zone != "cn-hangzhou-b" {
		t.Fatalf("ACK result = %+v", got)
	}
	if len(commands) != 2 {
		t.Fatalf("commands = %v, want one query per candidate", commands)
	}
}

func TestResolveACKQueriesUpgradeTargetsForCreatableCandidates(t *testing.T) {
	var commands []string
	query := func(_ context.Context, command string) (any, error) {
		commands = append(commands, command)
		switch {
		case strings.Contains(command, "--mode creatable"):
			return map[string]any{"versions": []any{
				map[string]any{"version": "1.36.1-aliyun.1", "creatable": true, "runtimes": []any{map[string]any{"name": "containerd", "version": "2.1.8"}}},
				map[string]any{"version": "1.34.3-aliyun.1", "creatable": true, "runtimes": []any{map[string]any{"name": "containerd", "version": "2.1.8"}}},
			}}, nil
		case strings.Contains(command, "--kubernetes-version 1.36.1-aliyun.1"):
			return map[string]any{"versions": []any{
				map[string]any{"version": "1.36.1-aliyun.1", "creatable": true},
			}}, nil
		case strings.Contains(command, "--kubernetes-version 1.34.3-aliyun.1"):
			return map[string]any{"versions": []any{
				map[string]any{"version": "1.34.3-aliyun.1", "creatable": true, "upgradable_versions": []any{"1.35.2-aliyun.1"}},
			}}, nil
		default:
			return nil, fmt.Errorf("unexpected query %q", command)
		}
	}

	got, err := ResolveACK(context.Background(), query, "cn-hangzhou", []string{"ManagedKubernetes"},
		[]string{"ack.version", "ack.upgrade_version", "ack.runtime", "ack.runtime_version"}, ECSResult{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != "1.34.3-aliyun.1" || got.UpgradeVersion != "1.35.2-aliyun.1" || got.RuntimeVersion != "2.1.8" {
		t.Fatalf("ACK result = %+v", got)
	}
	if len(commands) != 3 || !strings.Contains(commands[2], "--query-upgradable-version") {
		t.Fatalf("commands = %v, want creatable query followed by per-version upgrade queries", commands)
	}
}

func TestResolveACKReportsMissingUpgradePathSeparately(t *testing.T) {
	query := func(_ context.Context, command string) (any, error) {
		return map[string]any{"versions": []any{
			map[string]any{"version": "1.36.1-aliyun.1", "creatable": true},
		}}, nil
	}
	_, err := ResolveACK(context.Background(), query, "cn-hangzhou", []string{"ManagedKubernetes"},
		[]string{"ack.version", "ack.upgrade_version"}, ECSResult{})
	if err == nil || !IsNoCompatibleACKUpgradePath(err) {
		t.Fatalf("err = %v, want missing ACK upgrade path classification", err)
	}
}

func TestResolveACKDoesNotTreatUnrelatedIDAsVersion(t *testing.T) {
	query := func(_ context.Context, command string) (any, error) {
		if strings.Contains(command, "--cluster-type ManagedKubernetes") {
			return map[string]any{"versions": []any{
				map[string]any{"id": "resource-123", "creatable": true},
			}}, nil
		}
		return nil, fmt.Errorf("unexpected query %q", command)
	}
	_, err := ResolveACK(context.Background(), query, "cn-hangzhou", []string{"ManagedKubernetes"}, []string{"ack.cluster_type"}, ECSResult{})
	if err == nil || !strings.Contains(err.Error(), "no compatible creatable ACK") {
		t.Fatalf("err = %v, want unrelated resource ID to be rejected", err)
	}
}

func TestResolveLingjunSelectsAvailableNodeProfile(t *testing.T) {
	query := func(_ context.Context, command string) (any, error) {
		if !strings.Contains(command, "lingjun node list --free") {
			return nil, fmt.Errorf("unexpected query %q", command)
		}
		return map[string]any{"nodes": []any{
			map[string]any{"node_group": "ng-a", "hpn_zone": "hpn-a", "zone": "cn-hangzhou-b", "machine_type": "lingjun.g1xlarge", "image_id": "img-lite-1"},
			map[string]any{"node_group": "ng-b", "hpn_zone": "hpn-a", "zone": "cn-hangzhou-b", "machine_type": "lingjun.g1xlarge", "image_id": "img-lite-1"},
		}}, nil
	}
	got, err := ResolveLingjun(context.Background(), query, "cn-hangzhou", "Lite", []string{"ng-a", "ng-b"})
	if err != nil {
		t.Fatal(err)
	}
	if got.ClusterType != "Lite" || got.HPNZone != "hpn-a" || got.Zone != "cn-hangzhou-b" || got.MachineType != "lingjun.g1xlarge" || got.ImageID != "img-lite-1" {
		t.Fatalf("Lingjun result = %+v", got)
	}
}

func TestResolveLingjunSkipsNodeProfileWithWrongClusterType(t *testing.T) {
	query := func(_ context.Context, command string) (any, error) {
		switch {
		case strings.Contains(command, "lingjun node list --free"):
			return map[string]any{"nodes": []any{
				map[string]any{"cluster_type": "Standard", "node_group": "ng-a", "hpn_zone": "hpn-wrong", "zone": "cn-hangzhou-a", "machine_type": "wrong"},
				map[string]any{"cluster_type": "Lite", "node_group": "ng-a", "hpn_zone": "hpn-a", "zone": "cn-hangzhou-b", "machine_type": "lingjun.g1xlarge"},
				map[string]any{"cluster_type": "Lite", "node_group": "ng-b", "hpn_zone": "hpn-a", "zone": "cn-hangzhou-b", "machine_type": "lingjun.g1xlarge"},
			}}, nil
		default:
			return nil, fmt.Errorf("unexpected query %q", command)
		}
	}
	got, err := ResolveLingjun(context.Background(), query, "cn-hangzhou", "Lite", []string{"ng-a", "ng-b"})
	if err != nil {
		t.Fatal(err)
	}
	if got.MachineType != "lingjun.g1xlarge" || got.HPNZone != "hpn-a" {
		t.Fatalf("Lingjun result = %+v, want the Lite-compatible profile", got)
	}
}

func TestResolveLingjunRejectsMissingConfiguredNodeGroup(t *testing.T) {
	query := func(_ context.Context, command string) (any, error) {
		if strings.Contains(command, "lingjun node list --free") {
			return map[string]any{"nodes": []any{
				map[string]any{"node_group": "ng-a", "hpn_zone": "hpn-a", "zone": "cn-hangzhou-b", "machine_type": "lingjun.g1xlarge"},
			}}, nil
		}
		return nil, fmt.Errorf("unexpected query %q", command)
	}
	_, err := ResolveLingjun(context.Background(), query, "cn-hangzhou", "Lite", []string{"ng-a", "ng-b"})
	if err == nil || !strings.Contains(err.Error(), `node group "ng-b"`) {
		t.Fatalf("err = %v, want missing configured node-group failure", err)
	}
}

func TestResolveLingjunRejectsIncompatibleConfiguredNodeGroups(t *testing.T) {
	query := func(_ context.Context, command string) (any, error) {
		if strings.Contains(command, "lingjun node list --free") {
			return map[string]any{"nodes": []any{
				map[string]any{"node_group": "ng-a", "hpn_zone": "hpn-a", "zone": "cn-hangzhou-b", "machine_type": "lingjun.g1xlarge", "image_id": "img-lite-1"},
				map[string]any{"node_group": "ng-b", "hpn_zone": "hpn-b", "zone": "cn-hangzhou-c", "machine_type": "lingjun.g2xlarge", "image_id": "img-lite-2"},
			}}, nil
		}
		return nil, fmt.Errorf("unexpected query %q", command)
	}
	_, err := ResolveLingjun(context.Background(), query, "cn-hangzhou", "Lite", []string{"ng-a", "ng-b"})
	if err == nil || !strings.Contains(err.Error(), "compatible free-node profile") {
		t.Fatalf("err = %v, want incompatible node-group failure", err)
	}
}

func TestResolveLingjunRequiresTwoDistinctNodeGroups(t *testing.T) {
	query := func(_ context.Context, _ string) (any, error) {
		t.Fatal("invalid node-group config must fail before inventory queries")
		return nil, nil
	}
	for _, nodeGroupIDs := range [][]string{{"ng-a"}, {"ng-a", "ng-a"}} {
		if _, err := ResolveLingjun(context.Background(), query, "cn-hangzhou", "Lite", nodeGroupIDs); err == nil {
			t.Fatalf("node_group_ids %v should be rejected", nodeGroupIDs)
		}
	}
}

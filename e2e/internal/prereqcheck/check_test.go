package prereqcheck

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/fixtureconfig"
)

func TestCheckKeepsAvailableLingjunPrerequisites(t *testing.T) {
	fake, logPath := writeFake(t, `
case "$*" in
  *"ListFreeNodes"*) echo '{"response":{"Nodes":[{"NodeGroupId":"ng-a","HpnZone":"hpn-a","ZoneId":"cn-hangzhou-b","MachineType":"lingjun.g1xlarge","ImageId":"img-lite-1"},{"NodeGroupId":"ng-b","HpnZone":"hpn-a","ZoneId":"cn-hangzhou-b","MachineType":"lingjun.g1xlarge","ImageId":"img-lite-1"}]}}' ;;
  *) echo '{"error":{"code":"UnexpectedCommand","message":"unexpected command"}}'; exit 1 ;;
esac
`)
	profile := regionProfile("cn-hangzhou", map[string]map[string]any{
		"lingjun.cluster": {"node_group_ids": []any{"ng-a", "ng-b"}},
	})

	result, err := Check(context.Background(), Options{
		Profiles: []fixtureconfig.RegionProfile{profile},
		Required: map[string]bool{"lingjun.cluster": true},
		EcctlBin: fake,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Warnings) != 0 {
		t.Fatalf("warnings = %#v, want none", result.Warnings)
	}
	if !result.Profiles[0].HasPrerequisites([]string{"lingjun.cluster"}) {
		t.Fatalf("available prerequisites were removed: %#v", result.Profiles[0].Prerequisites)
	}
	if got := result.LingjunByRegion["cn-hangzhou"]; got.HPNZone != "hpn-a" || got.MachineType != "lingjun.g1xlarge" {
		t.Fatalf("Lingjun result = %#v", got)
	}
	log := readLog(t, logPath)
	if strings.Count(log, "call eflo-controller ListFreeNodes --MaxResults 100") != 1 {
		t.Fatalf("Lingjun probe count is not one:\n%s", log)
	}
}

func TestCheckKeepsMatchingLingjunClusterNetworkPrerequisite(t *testing.T) {
	fake, logPath := writeFake(t, `
echo '{"operation":"ListElasticNetworkInterfaces","product":"eflo","response":{"Code":0,"Content":{"Data":[{"ElasticNetworkInterfaceId":"leni-e2e","ResourceGroupId":"rg-e2e","VpcId":"vpc-e2e","VSwitchId":"vsw-e2e","SecurityGroupId":"sg-e2e","ZoneId":"cn-hangzhou-b","Status":"Unattached","Type":"CUSTOM"}],"Total":1},"Message":"成功"}}'
`)
	const prerequisite = "lingjun.cluster_network"
	profile := regionProfile("cn-hangzhou", map[string]map[string]any{
		prerequisite: {
			"eni_id":            "leni-e2e",
			"resource_group_id": "rg-e2e",
			"vpc_id":            "vpc-e2e",
			"vswitch_id":        "vsw-e2e",
			"security_group_id": "sg-e2e",
			"hpn_zone":          "B1",
			"zone":              "cn-hangzhou-b",
			"machine_type":      "efg2.C48vNHsbn",
			"image_id":          "img-e2e",
		},
	})

	result, err := Check(context.Background(), Options{
		Profiles: []fixtureconfig.RegionProfile{profile},
		Required: map[string]bool{prerequisite: true},
		EcctlBin: fake,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Warnings) != 0 || !result.Profiles[0].HasPrerequisites([]string{prerequisite}) {
		t.Fatalf("cluster network result = profiles %#v warnings %#v", result.Profiles, result.Warnings)
	}
	if log := readLog(t, logPath); !strings.Contains(log, "call eflo ListElasticNetworkInterfaces --ElasticNetworkInterfaceId leni-e2e --PageSize 10") {
		t.Fatalf("Lingjun cluster network probe missing:\n%s", log)
	}
}

func TestCheckRemovesUnavailableLingjunNodeGroupsAndWarns(t *testing.T) {
	fake, _ := writeFake(t, `
echo '{"nodes":[{"node_group":"ng-a","hpn_zone":"hpn-a","zone":"cn-hangzhou-b","machine_type":"lingjun.g1xlarge"}]}'
`)
	profile := regionProfile("cn-hangzhou", map[string]map[string]any{
		"lingjun.cluster": {"node_group_ids": []any{"ng-a", "ng-b"}},
	})

	result, err := Check(context.Background(), Options{
		Profiles: []fixtureconfig.RegionProfile{profile}, Required: map[string]bool{"lingjun.cluster": true}, EcctlBin: fake,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Profiles[0].HasPrerequisites([]string{"lingjun.cluster"}) {
		t.Fatalf("unavailable Lingjun prerequisite was retained: %#v", result.Profiles[0].Prerequisites)
	}
	if len(result.Warnings) != 1 || result.Warnings[0].Prerequisite != "lingjun.cluster" || !strings.Contains(result.Warnings[0].Reason, `node group "ng-b"`) {
		t.Fatalf("warnings = %#v", result.Warnings)
	}
}

func TestCheckKeepsCompleteDeclarativePrerequisites(t *testing.T) {
	prerequisites := map[string]map[string]any{
		ACKAutoRepairPolicy: {},
	}
	required := map[string]bool{}
	for name := range prerequisites {
		required[name] = true
	}
	result, err := Check(context.Background(), Options{
		Profiles: []fixtureconfig.RegionProfile{regionProfile("cn-hangzhou", prerequisites)},
		Required: required,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Warnings) != 0 {
		t.Fatalf("warnings = %#v, want none", result.Warnings)
	}
	for name := range prerequisites {
		if !result.Profiles[0].HasPrerequisites([]string{name}) {
			t.Fatalf("complete prerequisite %q was removed: %#v", name, result.Profiles[0].Prerequisites)
		}
	}
}

func TestFindErrorStringPrefersStructuredErrorEnvelope(t *testing.T) {
	value := map[string]any{
		"actions": []any{map[string]any{"code": "404, raw API error"}},
		"error":   map[string]any{"code": "NotFound"},
	}
	if got := findErrorString(value, "code"); got != "NotFound" {
		t.Fatalf("error code = %q, want NotFound", got)
	}
}

func TestCheckKeepsDisabledAccountSettingPrerequisites(t *testing.T) {
	fake, logPath := writeFake(t, `
case "$*" in
  *"rg associated-transfer list"*) echo '{"associated_transfer_settings":[{"status":"Disable"}]}' ;;
  *"rg notification get"*) echo '{"notification_setting":{"status":false}}' ;;
  *) echo '{"error":{"code":"UnexpectedCommand","message":"unexpected command"}}'; exit 1 ;;
esac
`)
	profile := regionProfile("cn-hangzhou", map[string]map[string]any{
		RGAssociatedTransferDisabled: {},
		RGNotificationDisabled:       {},
	})
	result, err := Check(context.Background(), Options{
		Profiles: []fixtureconfig.RegionProfile{profile},
		Required: map[string]bool{
			RGAssociatedTransferDisabled: true,
			RGNotificationDisabled:       true,
		},
		EcctlBin: fake,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Warnings) != 0 {
		t.Fatalf("warnings = %#v, want none", result.Warnings)
	}
	for _, prerequisite := range []string{RGAssociatedTransferDisabled, RGNotificationDisabled} {
		if !result.Profiles[0].HasPrerequisites([]string{prerequisite}) {
			t.Fatalf("prerequisite %q was removed: %#v", prerequisite, result.Profiles[0].Prerequisites)
		}
	}
	log := readLog(t, logPath)
	for _, command := range []string{"rg associated-transfer list", "rg notification get"} {
		if !strings.Contains(log, command) {
			t.Fatalf("probe %q missing:\n%s", command, log)
		}
	}
}

func TestCheckRemovesEnabledAccountSettingPrerequisites(t *testing.T) {
	fake, _ := writeFake(t, `
case "$*" in
  *"rg associated-transfer list"*) echo '{"associated_transfer_settings":[{"status":"Enable"}]}' ;;
  *"rg notification get"*) echo '{"notification_setting":{"status":true}}' ;;
  *) echo '{"error":{"code":"UnexpectedCommand","message":"unexpected command"}}'; exit 1 ;;
esac
`)
	profile := regionProfile("cn-hangzhou", map[string]map[string]any{
		RGAssociatedTransferDisabled: {},
		RGNotificationDisabled:       {},
	})
	result, err := Check(context.Background(), Options{
		Profiles: []fixtureconfig.RegionProfile{profile},
		Required: map[string]bool{
			RGAssociatedTransferDisabled: true,
			RGNotificationDisabled:       true,
		},
		EcctlBin: fake,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Warnings) != 2 {
		t.Fatalf("warnings = %#v, want two", result.Warnings)
	}
	for _, prerequisite := range []string{RGAssociatedTransferDisabled, RGNotificationDisabled} {
		if result.Profiles[0].HasPrerequisites([]string{prerequisite}) {
			t.Fatalf("unsafe prerequisite %q retained: %#v", prerequisite, result.Profiles[0].Prerequisites)
		}
	}
}

func TestCheckAutoDiscoversLingjunNetwork(t *testing.T) {
	fake, logPath := writeFake(t, `
echo '{"request":"DescribeZones","zones":[{"ZoneId":"cn-hangzhou-b"},{"ZoneId":"cn-hangzhou-alipay-b"}]}'
`)
	profile := regionProfile("cn-hangzhou", nil)

	result, err := Check(context.Background(), Options{
		Profiles: []fixtureconfig.RegionProfile{profile},
		Required: map[string]bool{"lingjun.network": true},
		EcctlBin: fake,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Warnings) != 0 {
		t.Fatalf("warnings = %#v, want none", result.Warnings)
	}
	if !result.Profiles[0].HasPrerequisites([]string{"lingjun.network"}) {
		t.Fatalf("lingjun.network was not injected: %#v", result.Profiles[0].Prerequisites)
	}
	zone, ok := result.Profiles[0].Prerequisites["lingjun.network"]["zone"].(string)
	if !ok || zone != "cn-hangzhou-b" {
		t.Fatalf("zone = %q, want cn-hangzhou-b", zone)
	}
	log := readLog(t, logPath)
	if !strings.Contains(log, "call eflo-controller DescribeZones") {
		t.Fatalf("DescribeZones probe missing:\n%s", log)
	}
}

func TestCheckRemovesLingjunNetworkOnDiscoveryFailure(t *testing.T) {
	fake, _ := writeFake(t, `
echo '{"error":{"code":"InvalidRegionId","message":"region not supported"}}'
exit 1
`)
	profile := regionProfile("cn-zhangjiakou", nil)

	result, err := Check(context.Background(), Options{
		Profiles: []fixtureconfig.RegionProfile{profile},
		Required: map[string]bool{"lingjun.network": true},
		EcctlBin: fake,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Profiles[0].HasPrerequisites([]string{"lingjun.network"}) {
		t.Fatalf("lingjun.network was retained despite discovery failure: %#v", result.Profiles[0].Prerequisites)
	}
	if len(result.Warnings) != 1 || result.Warnings[0].Prerequisite != "lingjun.network" {
		t.Fatalf("warnings = %#v, want one lingjun.network warning", result.Warnings)
	}
}

func TestCheckAutoResolvesLingjunClusterNetwork(t *testing.T) {
	fake, logPath := writeFake(t, `
case "$*" in
  *"DescribeZones"*) echo '{"zones":[{"ZoneId":"cn-zhangjiakou-d"},{"ZoneId":"cn-zhangjiakou-alipay-d"}]}' ;;
  *"ListMachineTypes"*) echo '{"machine_types":[{"Name":"efg2.C48vNHsbn","Type":"Public"},{"Name":"efg2.C48vNHsbn-ng","Type":"NonPublic"}]}' ;;
  *"ListImages"*) echo '{"images":[{"ImageId":"img-e2e","ImageVersion":"2.1.0","Description":"efg2.C48vNHsbn public image"}]}' ;;
  *"ListMachineNetworkInfo"*) echo '{"response":{"MachineNetworkInfo":[{"HpnZone":"D1","MachineType":"efg2.C48vNHsbn"}]}}' ;;
  *"rg group list"*) echo '{"groups":[{"Id":"rg-default","Name":"Default","Status":"OK"},{"Id":"rg-other","Name":"Other","Status":"OK"}]}' ;;
  *) echo '{"error":{"code":"UnexpectedCommand","message":"unexpected command"}}'; exit 1 ;;
esac
`)
	const prerequisite = "lingjun.cluster_network"
	profile := regionProfile("cn-zhangjiakou", nil)

	result, err := Check(context.Background(), Options{
		Profiles: []fixtureconfig.RegionProfile{profile},
		Required: map[string]bool{prerequisite: true},
		EcctlBin: fake,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Warnings) != 0 {
		t.Fatalf("warnings = %#v, want none", result.Warnings)
	}
	bundle := result.Profiles[0].Prerequisites[prerequisite]
	for field, want := range map[string]string{
		"zone":              "cn-zhangjiakou-d",
		"hpn_zone":          "D1",
		"machine_type":      "efg2.C48vNHsbn",
		"image_id":          "img-e2e",
		"resource_group_id": "rg-default",
	} {
		if got, _ := bundle[field].(string); got != want {
			t.Fatalf("%s = %q, want %q (bundle %#v)", field, got, want, bundle)
		}
	}
	log := readLog(t, logPath)
	if !strings.Contains(log, "rg group list") {
		t.Fatalf("resource group probe missing:\n%s", log)
	}
}

func TestCheckFailsOnLingjunClusterNetworkProbeFatalError(t *testing.T) {
	fake, _ := writeFake(t, `
echo '{"error":{"code":"InvalidRegionId","message":"region not supported"}}'
exit 1
`)
	profile := regionProfile("cn-zhangjiakou", nil)

	_, err := Check(context.Background(), Options{
		Profiles: []fixtureconfig.RegionProfile{profile},
		Required: map[string]bool{"lingjun.cluster_network": true},
		EcctlBin: fake,
	})
	if err == nil || !strings.Contains(err.Error(), "InvalidRegionId") {
		t.Fatalf("err = %v, want fatal InvalidRegionId", err)
	}
}

func TestDeriveHPNZone(t *testing.T) {
	for zone, want := range map[string]string{
		"cn-hangzhou-b":    "B1",
		"cn-zhangjiakou-d": "D1",
		"cn-qingdao-c":     "C1",
	} {
		got, err := deriveHPNZone(zone)
		if err != nil || got != want {
			t.Fatalf("deriveHPNZone(%q) = %q, %v; want %q", zone, got, err, want)
		}
	}
	for _, zone := range []string{"cn-hangzhou", "cn-hangzhou-", "cn-hangzhou-1"} {
		if got, err := deriveHPNZone(zone); err == nil {
			t.Fatalf("deriveHPNZone(%q) = %q, want error", zone, got)
		}
	}
}

func TestCheckTreatsProbePermissionErrorsAsFatal(t *testing.T) {
	for _, test := range []struct {
		name         string
		required     string
		prerequisite map[string]any
	}{
		{name: "Lingjun", required: "lingjun.cluster", prerequisite: map[string]any{"node_group_ids": []any{"ng-a", "ng-b"}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			fake, _ := writeFake(t, `
echo '{"error":{"code":"NoPermission","message":"not authorized"}}'
exit 1
`)
			profile := regionProfile("cn-hangzhou", map[string]map[string]any{test.required: test.prerequisite})
			_, err := Check(context.Background(), Options{
				Profiles: []fixtureconfig.RegionProfile{profile}, Required: map[string]bool{test.required: true}, EcctlBin: fake,
			})
			if err == nil || !strings.Contains(err.Error(), "NoPermission") {
				t.Fatalf("err = %v, want fatal NoPermission", err)
			}
		})
	}
}

func regionProfile(id string, prerequisites map[string]map[string]any) fixtureconfig.RegionProfile {
	return fixtureconfig.RegionProfile{ID: id, Prerequisites: prerequisites}
}

func writeFake(t *testing.T, body string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "ecctl")
	logPath := filepath.Join(dir, "calls.log")
	script := "#!/bin/sh\necho \"$*\" >> \"$FAKE_LOG\"\n" + body
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_LOG", logPath)
	return path, logPath
}

func readLog(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

package prereqcheck

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ecctl/e2e/internal/fixtureconfig"
)

func TestCheckKeepsAvailableOSSAndLingjunPrerequisites(t *testing.T) {
	fake, logPath := writeFake(t, `
case "$*" in
  *"resourcecenter GetResourceConfiguration"*) echo '{"response":{"ResourceId":"bucket-e2e","RegionId":"cn-hangzhou","ResourceType":"ACS::OSS::Bucket"}}' ;;
  *"lingjun node list"*) echo '{"nodes":[{"node_group":"ng-a","hpn_zone":"hpn-a","zone":"cn-hangzhou-b","machine_type":"lingjun.g1xlarge","image_id":"img-lite-1"},{"node_group":"ng-b","hpn_zone":"hpn-a","zone":"cn-hangzhou-b","machine_type":"lingjun.g1xlarge","image_id":"img-lite-1"}]}' ;;
  *) echo '{"error":{"code":"UnexpectedCommand","message":"unexpected command"}}'; exit 1 ;;
esac
`)
	profile := regionProfile("cn-hangzhou", map[string]map[string]any{
		"ecs.image":       {"oss_bucket": "bucket-e2e"},
		"lingjun.cluster": {"node_group_ids": []any{"ng-a", "ng-b"}},
	})

	result, err := Check(context.Background(), Options{
		Profiles: []fixtureconfig.RegionProfile{profile},
		Required: map[string]bool{"ecs.image": true, "lingjun.cluster": true},
		EcctlBin: fake,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Warnings) != 0 {
		t.Fatalf("warnings = %#v, want none", result.Warnings)
	}
	if !result.Profiles[0].HasPrerequisites([]string{"ecs.image", "lingjun.cluster"}) {
		t.Fatalf("available prerequisites were removed: %#v", result.Profiles[0].Prerequisites)
	}
	if got := result.LingjunByRegion["cn-hangzhou"]; got.HPNZone != "hpn-a" || got.MachineType != "lingjun.g1xlarge" {
		t.Fatalf("Lingjun result = %#v", got)
	}
	log := readLog(t, logPath)
	if !strings.Contains(log, "GetResourceConfiguration --ResourceId bucket-e2e --ResourceRegionId cn-hangzhou --ResourceType ACS::OSS::Bucket") {
		t.Fatalf("OSS probe command missing:\n%s", log)
	}
	if strings.Count(log, "lingjun node list --free --limit 100") != 1 {
		t.Fatalf("Lingjun probe count is not one:\n%s", log)
	}
}

func TestCheckRemovesMissingOSSBucketAndWarns(t *testing.T) {
	fake, _ := writeFake(t, `
echo '{"error":{"code":"NotExists.Resource","message":"resource does not exist"}}'
exit 1
`)
	profile := regionProfile("cn-hangzhou", map[string]map[string]any{
		"ecs.image": {"oss_bucket": "missing-bucket"},
		"future":    {"id": "keep-me"},
	})

	result, err := Check(context.Background(), Options{
		Profiles: []fixtureconfig.RegionProfile{profile}, Required: map[string]bool{"ecs.image": true}, EcctlBin: fake,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Profiles[0].HasPrerequisites([]string{"ecs.image"}) {
		t.Fatalf("missing OSS prerequisite was retained: %#v", result.Profiles[0].Prerequisites)
	}
	if !result.Profiles[0].HasPrerequisites([]string{"future"}) {
		t.Fatalf("unrelated prerequisite was removed: %#v", result.Profiles[0].Prerequisites)
	}
	if len(result.Warnings) != 1 || result.Warnings[0].Prerequisite != "ecs.image" || !strings.Contains(result.Warnings[0].Reason, "does not exist") {
		t.Fatalf("warnings = %#v", result.Warnings)
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

func TestCheckRemovesRootAccountPrerequisiteForRAMUserAndWarns(t *testing.T) {
	fake, logPath := writeFake(t, `
echo '{"response":{"AccountId":"1754580903499898","IdentityType":"RAMUser","UserId":"229050894712721687"}}'
`)
	profile := regionProfile("cn-heyuan", map[string]map[string]any{
		"ack.root_account": {},
		"future":           {"id": "keep-me"},
	})

	result, err := Check(context.Background(), Options{
		Profiles: []fixtureconfig.RegionProfile{profile}, Required: map[string]bool{"ack.root_account": true}, EcctlBin: fake,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Profiles[0].HasPrerequisites([]string{"ack.root_account"}) {
		t.Fatalf("RAM user retained root-account prerequisite: %#v", result.Profiles[0].Prerequisites)
	}
	if !result.Profiles[0].HasPrerequisites([]string{"future"}) {
		t.Fatalf("unrelated prerequisite was removed: %#v", result.Profiles[0].Prerequisites)
	}
	if len(result.Warnings) != 1 || result.Warnings[0].Prerequisite != "ack.root_account" || !strings.Contains(result.Warnings[0].Reason, "RAMUser") {
		t.Fatalf("warnings = %#v", result.Warnings)
	}
	if log := readLog(t, logPath); !strings.Contains(log, "call sts GetCallerIdentity") {
		t.Fatalf("STS identity probe missing:\n%s", log)
	}
}

func TestCheckKeepsRootAccountPrerequisiteForAccount(t *testing.T) {
	fake, _ := writeFake(t, `
echo '{"response":{"AccountId":"1754580903499898","IdentityType":"Account","UserId":"1754580903499898"}}'
`)
	profile := regionProfile("cn-heyuan", map[string]map[string]any{"ack.root_account": {}})

	result, err := Check(context.Background(), Options{
		Profiles: []fixtureconfig.RegionProfile{profile}, Required: map[string]bool{"ack.root_account": true}, EcctlBin: fake,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Warnings) != 0 || !result.Profiles[0].HasPrerequisites([]string{"ack.root_account"}) {
		t.Fatalf("root-account prerequisite result = profiles %#v warnings %#v", result.Profiles, result.Warnings)
	}
}

func TestCheckTreatsProbePermissionErrorsAsFatal(t *testing.T) {
	for _, test := range []struct {
		name         string
		required     string
		prerequisite map[string]any
	}{
		{name: "OSS", required: "ecs.image", prerequisite: map[string]any{"oss_bucket": "bucket-e2e"}},
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

package spec_resource

import (
	"testing"
)

// keypair -------------------------------------------------------------------

func fakeKeyPairListResponse(name string) map[string]any {
	return map[string]any{
		"RequestId":  "req-keypairs",
		"TotalCount": 1,
		"KeyPairs": map[string]any{"KeyPair": []any{
			map[string]any{"KeyPairName": name, "KeyPairId": "kp-1", "KeyPairFingerPrint": "ab:cd:ef"},
		}},
	}
}

func TestECSKeypairCreateUsesCreateKeyPair(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{
		{"RequestId": "req-create", "KeyPairName": "web-key", "KeyPairId": "kp-1"},
		fakeKeyPairListResponse("web-key"),
	}}
	runCLI := catalogCaller(t, "ecs", "keypair", fake)

	stdout, stderr, code := runCLI("ecs", "keypair", "create", "--region", "cn-hangzhou", "--name", "web-key")
	if code != 0 {
		t.Fatalf("ecs keypair create exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 2 || fake.calls[0].operation != "CreateKeyPair" || fake.calls[1].operation != "DescribeKeyPairs" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	if fake.calls[0].request["KeyPairName"] != "web-key" {
		t.Fatalf("CreateKeyPair request = %#v", fake.calls[0].request)
	}
}

func TestECSKeypairCreateWithPublicKeyImports(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{
		{"RequestId": "req-import", "KeyPairName": "imported"},
		fakeKeyPairListResponse("imported"),
	}}
	runCLI := catalogCaller(t, "ecs", "keypair", fake)

	stdout, stderr, code := runCLI("ecs", "keypair", "create", "--region", "cn-hangzhou", "--name", "imported", "--public-key", "ssh-rsa AAAAB3")
	if code != 0 {
		t.Fatalf("ecs keypair import exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 2 || fake.calls[0].operation != "ImportKeyPair" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	if fake.calls[0].request["PublicKeyBody"] != "ssh-rsa AAAAB3" {
		t.Fatalf("ImportKeyPair request = %#v", fake.calls[0].request)
	}
}

func TestECSKeypairDeleteUsesDeleteKeyPairs(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{{"RequestId": "req-delete"}}}
	runCLI := catalogCaller(t, "ecs", "keypair", fake)

	stdout, stderr, code := runCLI("ecs", "keypair", "delete", "k1", "k2", "--region", "cn-hangzhou")
	if code != 0 {
		t.Fatalf("ecs keypair delete exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "DeleteKeyPairs" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	if fake.calls[0].request["KeyPairNames"] != `["k1","k2"]` {
		t.Fatalf("KeyPairNames = %#v, want JSON array string", fake.calls[0].request["KeyPairNames"])
	}
}

func sliceContainsValues(value any, want ...string) bool {
	var got []string
	switch typed := value.(type) {
	case []string:
		got = typed
	case []any:
		for _, v := range typed {
			if s, ok := v.(string); ok {
				got = append(got, s)
			}
		}
	default:
		return false
	}
	if len(got) != len(want) {
		return false
	}
	for i := range want {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// assistant -----------------------------------------------------------------

func TestECSAssistantGetReadsSettings(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{{
		"RequestId":            "req-settings",
		"AgentUpgradeConfig":   map[string]any{"AllowedUpgradeWindows": map[string]any{"AllowedUpgradeWindow": []any{}}},
		"SessionManagerConfig": map[string]any{"SessionManagerEnabled": true},
		"OssDeliveryConfigs":   map[string]any{"OssDeliveryConfig": []any{map[string]any{"DeliveryType": "Invocation", "Enabled": false}}},
	}}}
	runCLI := catalogCaller(t, "ecs", "assistant", fake)

	stdout, stderr, code := runCLI("ecs", "assistant", "get", "--region", "cn-hangzhou")
	if code != 0 {
		t.Fatalf("ecs assistant get exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "DescribeCloudAssistantSettings" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	// SettingType is mandatory; the setting_types control's default must supply it.
	if fake.calls[0].request["SettingType.1"] != "InvocationDelivery" {
		t.Fatalf("SettingType default not supplied: %#v", fake.calls[0].request)
	}
	assistant, _ := decodeObject(t, stdout)["assistant"].(map[string]any)
	if assistant == nil || assistant["session_manager_enabled"] != true {
		t.Fatalf("unexpected assistant settings: %s", stdout)
	}
	if enabled, ok := assistant["agent_upgrade_enabled"].(bool); !ok || enabled {
		t.Fatalf("missing AgentUpgradeConfig.Enabled must normalize to false: %s", stdout)
	}
}

func TestECSAssistantUpdateModifiesSettingsAndRereads(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{
		{"RequestId": "req-modify"},
		{"RequestId": "req-settings", "OssBucketName": "logs2"},
	}}
	runCLI := catalogCaller(t, "ecs", "assistant", fake)

	stdout, stderr, code := runCLI(
		"ecs", "assistant", "update",
		"--region", "cn-hangzhou",
		"--setting-type", "AgentUpgradeConfig",
		"--api-param", "AgentUpgradeConfig.Enabled=false",
	)
	if code != 0 {
		t.Fatalf("ecs assistant update exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 2 || fake.calls[0].operation != "ModifyCloudAssistantSettings" || fake.calls[1].operation != "DescribeCloudAssistantSettings" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	if fake.calls[0].request["SettingType"] != "AgentUpgradeConfig" || fake.calls[0].request["AgentUpgradeConfig.Enabled"] != "false" {
		t.Fatalf("ModifyCloudAssistantSettings api-param not forwarded: %#v", fake.calls[0].request)
	}
	if fake.calls[1].request["SettingType.1"] != "AgentUpgradeConfig" {
		t.Fatalf("DescribeCloudAssistantSettings must reread the updated setting: %#v", fake.calls[1].request)
	}
}

func TestECSAssistantInstallTargetsInstances(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{{"RequestId": "req-install"}}}
	runCLI := catalogCaller(t, "ecs", "assistant", fake)

	stdout, stderr, code := runCLI("ecs", "assistant", "install", "--region", "cn-hangzhou", "--instance-ids", `["i-123"]`, "--no-wait")
	if code != 0 {
		t.Fatalf("ecs assistant install exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "InstallCloudAssistant" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	if fake.calls[0].request["InstanceId.1"] != "i-123" {
		t.Fatalf("InstallCloudAssistant must expand InstanceId.N: %#v", fake.calls[0].request)
	}
}

func TestECSAssistantInstallDefaultWaitsForAgentStatus(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{
		{"RequestId": "req-install"},
		{"RequestId": "req-status", "InstanceCloudAssistantStatusSet": map[string]any{"InstanceCloudAssistantStatus": []any{
			map[string]any{"InstanceId": "i-123", "CloudAssistantStatus": "false"},
		}}},
	}}
	runCLI := catalogCaller(t, "ecs", "assistant", fake)

	stdout, stderr, code := runCLI("ecs", "assistant", "install", "--region", "cn-hangzhou", "--instance-ids", `["i-123"]`, "--timeout", "1ms")
	if code == 0 {
		t.Fatalf("ecs assistant install should wait for CloudAssistantStatus=true and time out; stdout=%s stderr=%s", stdout, stderr)
	}
	if got := errorCode(t, stdout); got != "WaitTimeout" {
		t.Fatalf("error code = %q, want WaitTimeout; stdout=%s stderr=%s", got, stdout, stderr)
	}
	// The 1ms timeout can elapse after one or more polls, so assert at least one
	// DescribeCloudAssistantStatus poll rather than an exact call count.
	if len(fake.calls) < 2 || fake.calls[0].operation != "InstallCloudAssistant" || fake.calls[1].operation != "DescribeCloudAssistantStatus" {
		t.Fatalf("calls = %#v", fake.calls)
	}
}

// auto-snapshot-policy -------------------------------------------------------

func TestECSAutoSnapshotPolicyCreatePassesJSONArrayParams(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{{"RequestId": "req-create", "AutoSnapshotPolicyId": "sp-1"}}}
	runCLI := catalogCaller(t, "ecs", "auto-snapshot-policy", fake)

	stdout, stderr, code := runCLI(
		"ecs", "auto-snapshot-policy", "create",
		"--region", "cn-hangzhou",
		"--name", "daily",
		"--time-points", `["0","12"]`,
		"--repeat-weekdays", `["1","7"]`,
		"--retention-days", "7",
		"--no-wait",
	)
	if code != 0 {
		t.Fatalf("ecs auto-snapshot-policy create exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "CreateAutoSnapshotPolicy" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	req := fake.calls[0].request
	if req["regionId"] != "cn-hangzhou" || req["timePoints"] != `["0","12"]` || req["repeatWeekdays"] != `["1","7"]` || req["retentionDays"] != 7 {
		t.Fatalf("CreateAutoSnapshotPolicy lowercase params wrong: %#v", req)
	}
}

func TestECSAutoSnapshotPolicyUpdateAttachRoutesApply(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{{"RequestId": "req-apply"}}}
	runCLI := catalogCaller(t, "ecs", "auto-snapshot-policy", fake)

	stdout, stderr, code := runCLI("ecs", "auto-snapshot-policy", "update", "sp-1", "--region", "cn-hangzhou", "--attach-disk-id", `["d-1"]`, "--no-wait")
	if code != 0 {
		t.Fatalf("ecs asp update attach exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "ApplyAutoSnapshotPolicy" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	// diskIds is JSON-array-serialised by the aliyun caller (key ends in "Ids");
	// the engine hands the fake the raw slice.
	if !sliceContainsValues(fake.calls[0].request["diskIds"], "d-1") {
		t.Fatalf("ApplyAutoSnapshotPolicy diskIds = %#v, want slice [d-1]", fake.calls[0].request["diskIds"])
	}
	if fake.calls[0].request["regionId"] != "cn-hangzhou" {
		t.Fatalf("ApplyAutoSnapshotPolicy regionId = %#v", fake.calls[0].request)
	}
}

func TestECSAutoSnapshotPolicyUpdateAttributesUsesLowercaseRegion(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{{"RequestId": "req-update"}}}
	runCLI := catalogCaller(t, "ecs", "auto-snapshot-policy", fake)

	stdout, stderr, code := runCLI("ecs", "auto-snapshot-policy", "update", "sp-1", "--region", "cn-hangzhou", "--name", "renamed", "--no-wait")
	if code != 0 {
		t.Fatalf("ecs asp update exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "ModifyAutoSnapshotPolicyEx" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	if got := fake.calls[0].request["regionId"]; got != "cn-hangzhou" {
		t.Fatalf("ModifyAutoSnapshotPolicyEx regionId = %#v; request=%#v", got, fake.calls[0].request)
	}
	if _, exists := fake.calls[0].request["RegionId"]; exists {
		t.Fatalf("ModifyAutoSnapshotPolicyEx must not use RegionId: %#v", fake.calls[0].request)
	}
}

func TestECSAutoSnapshotPolicyDeleteUsesLowercaseRegion(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{{"RequestId": "req-delete"}}}
	runCLI := catalogCaller(t, "ecs", "auto-snapshot-policy", fake)

	stdout, stderr, code := runCLI("ecs", "auto-snapshot-policy", "delete", "sp-1", "--region", "cn-hangzhou")
	if code != 0 {
		t.Fatalf("ecs asp delete exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "DeleteAutoSnapshotPolicy" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	if got := fake.calls[0].request["regionId"]; got != "cn-hangzhou" {
		t.Fatalf("DeleteAutoSnapshotPolicy regionId = %#v; request=%#v", got, fake.calls[0].request)
	}
	if _, exists := fake.calls[0].request["RegionId"]; exists {
		t.Fatalf("DeleteAutoSnapshotPolicy must not use RegionId: %#v", fake.calls[0].request)
	}
}

func TestECSAutoSnapshotPolicyUpdateDetachRoutesCancel(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{{"RequestId": "req-cancel"}}}
	runCLI := catalogCaller(t, "ecs", "auto-snapshot-policy", fake)

	stdout, stderr, code := runCLI("ecs", "auto-snapshot-policy", "update", "sp-1", "--region", "cn-hangzhou", "--detach-disk-id", `["d-1"]`, "--no-wait")
	if code != 0 {
		t.Fatalf("ecs asp update detach exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "CancelAutoSnapshotPolicy" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	if fake.calls[0].request["regionId"] != "cn-hangzhou" {
		t.Fatalf("CancelAutoSnapshotPolicy regionId = %#v", fake.calls[0].request)
	}
}

// snapshot-group ------------------------------------------------------------

func TestECSSnapshotGroupCreateUsesCreateSnapshotGroup(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{{"RequestId": "req-create", "SnapshotGroupId": "ssg-1"}}}
	runCLI := catalogCaller(t, "ecs", "snapshot-group", fake)

	stdout, stderr, code := runCLI("ecs", "snapshot-group", "create", "--region", "cn-hangzhou", "--instance", "i-1", "--name", "grp", "--no-wait")
	if code != 0 {
		t.Fatalf("ecs snapshot-group create exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "CreateSnapshotGroup" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	if fake.calls[0].request["InstanceId"] != "i-1" {
		t.Fatalf("CreateSnapshotGroup request = %#v", fake.calls[0].request)
	}
	if _, ok := fake.calls[0].request["ClientToken"]; !ok {
		t.Fatalf("CreateSnapshotGroup must use ClientToken idempotency: %#v", fake.calls[0].request)
	}
}

func TestECSSnapshotGroupListUsesNextTokenPagination(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{{
		"RequestId": "req-list", "NextToken": "tok2",
		"SnapshotGroups": map[string]any{"SnapshotGroup": []any{map[string]any{"SnapshotGroupId": "ssg-1", "Status": "accomplished"}}},
	}}}
	runCLI := catalogCaller(t, "ecs", "snapshot-group", fake)

	stdout, stderr, code := runCLI("ecs", "snapshot-group", "list", "--region", "cn-hangzhou", "--limit", "50", "--next-token", "tok1")
	if code != 0 {
		t.Fatalf("ecs snapshot-group list exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "DescribeSnapshotGroups" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	req := fake.calls[0].request
	if req["MaxResults"] != 50 || req["NextToken"] != "tok1" {
		t.Fatalf("DescribeSnapshotGroups token pagination wrong: %#v", req)
	}
	groups, _ := decodeObject(t, stdout)["snapshot_groups"].([]any)
	if len(groups) != 1 {
		t.Fatalf("snapshot_groups = %#v; stdout=%s", groups, stdout)
	}
}

// snapshot ------------------------------------------------------------------

func fakeSnapshotListResponse(id, status string) map[string]any {
	return map[string]any{
		"RequestId":  "req-snap",
		"TotalCount": 1,
		"Snapshots": map[string]any{"Snapshot": []any{
			map[string]any{"SnapshotId": id, "SnapshotName": "backup", "Status": status, "Progress": "100%", "SourceDiskId": "d-1"},
		}},
	}
}

func TestECSSnapshotCreateUsesCreateSnapshot(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{{"RequestId": "req-create", "SnapshotId": "s-1"}}}
	runCLI := catalogCaller(t, "ecs", "snapshot", fake)

	stdout, stderr, code := runCLI("ecs", "snapshot", "create", "--region", "cn-hangzhou", "--disk", "d-1", "--name", "backup", "--no-wait")
	if code != 0 {
		t.Fatalf("ecs snapshot create exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "CreateSnapshot" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	if fake.calls[0].request["DiskId"] != "d-1" {
		t.Fatalf("CreateSnapshot request = %#v", fake.calls[0].request)
	}
}

func TestECSSnapshotUpdateLockRoutesLockSnapshot(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{{"RequestId": "req-lock"}}}
	runCLI := catalogCaller(t, "ecs", "snapshot", fake)

	stdout, stderr, code := runCLI("ecs", "snapshot", "update", "s-1", "--region", "cn-hangzhou", "--lock", "--no-wait")
	if code != 0 {
		t.Fatalf("ecs snapshot update lock exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "LockSnapshot" {
		t.Fatalf("calls = %#v", fake.calls)
	}
}

func TestECSSnapshotUpdateCategoryRoutesModifyCategory(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{{"RequestId": "req-cat"}}}
	runCLI := catalogCaller(t, "ecs", "snapshot", fake)

	stdout, stderr, code := runCLI("ecs", "snapshot", "update", "s-1", "--region", "cn-hangzhou", "--category", "flash", "--no-wait")
	if code != 0 {
		t.Fatalf("ecs snapshot update category exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "ModifySnapshotCategory" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	if fake.calls[0].request["Category"] != "flash" {
		t.Fatalf("ModifySnapshotCategory request = %#v", fake.calls[0].request)
	}
}

func TestECSSnapshotUpdateRejectsLockUnlockTogether(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{}
	runCLI := catalogCaller(t, "ecs", "snapshot", fake)

	stdout, _, code := runCLI("ecs", "snapshot", "update", "s-1", "--region", "cn-hangzhou", "--lock", "--unlock", "--no-wait")
	if code == 0 {
		t.Fatalf("snapshot update --lock --unlock should be rejected; stdout=%s", stdout)
	}
	if got := errorCode(t, stdout); got != "ConflictingParameters" {
		t.Fatalf("error.code = %q, want ConflictingParameters; stdout=%s", got, stdout)
	}
	if len(fake.calls) != 0 {
		t.Fatalf("conflict should fail before API calls: %#v", fake.calls)
	}
}

func TestECSSnapshotCopyUsesCopySnapshot(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{
		{"RequestId": "req-copy", "SnapshotId": "s-copy"},
		fakeSnapshotListResponse("s-copy", "accomplished"),
	}}
	runCLI := catalogCaller(t, "ecs", "snapshot", fake)

	stdout, stderr, code := runCLI("ecs", "snapshot", "copy", "s-1", "--region", "cn-hangzhou", "--destination-region", "cn-beijing", "--destination-name", "backup-copy")
	if code != 0 {
		t.Fatalf("ecs snapshot copy exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 2 || fake.calls[0].operation != "CopySnapshot" || fake.calls[1].operation != "DescribeSnapshots" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	req := fake.calls[0].request
	if req["SnapshotId"] != "s-1" || req["DestinationRegionId"] != "cn-beijing" || req["DestinationSnapshotName"] != "backup-copy" {
		t.Fatalf("CopySnapshot request = %#v", req)
	}
	readback := fake.calls[1].request
	if readback["RegionId"] != "cn-beijing" || !sliceContainsValues(readback["SnapshotIds"], "s-copy") {
		t.Fatalf("DescribeSnapshots destination readback request = %#v", readback)
	}
	snapshot, _ := decodeObject(t, stdout)["snapshot"].(map[string]any)
	if snapshot == nil || snapshot["id"] != "s-copy" || snapshot["status"] != "accomplished" {
		t.Fatalf("unexpected copy output: %s", stdout)
	}
}

func TestECSSnapshotDeleteForceIsOptIn(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{{"RequestId": "req-delete"}}}
	runCLI := catalogCaller(t, "ecs", "snapshot", fake)

	stdout, stderr, code := runCLI("ecs", "snapshot", "delete", "s-1", "--region", "cn-hangzhou", "--no-wait")
	if code != 0 {
		t.Fatalf("ecs snapshot delete exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "DeleteSnapshot" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	if got, ok := fake.calls[0].request["Force"]; ok && got == true {
		t.Fatalf("DeleteSnapshot Force must default to false: %#v", fake.calls[0].request)
	}
}

func TestECSSnapshotGetWithUsageAddsUsageProbe(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{
		fakeSnapshotListResponse("s-1", "accomplished"),
		{"RequestId": "req-usage", "SnapshotCount": 3, "SnapshotSize": 1024},
	}}
	runCLI := catalogCaller(t, "ecs", "snapshot", fake)

	stdout, stderr, code := runCLI("ecs", "snapshot", "get", "s-1", "--region", "cn-hangzhou", "--with-usage")
	if code != 0 {
		t.Fatalf("ecs snapshot get --with-usage exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 2 || fake.calls[0].operation != "DescribeSnapshots" || fake.calls[1].operation != "DescribeSnapshotsUsage" {
		t.Fatalf("calls = %#v", fake.calls)
	}
}

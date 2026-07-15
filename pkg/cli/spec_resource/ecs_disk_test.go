package spec_resource

import (
	"strings"
	"testing"

	"ecctl/pkg/engine"
	ecerrors "ecctl/pkg/errors"
	"ecctl/pkg/spec"
)

func TestECSDiskCreateUsesSpecDrivenCaller(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{
		responses: []map[string]any{
			{"RequestId": "req-create", "DiskId": "d-123"},
			fakeDiskListResponse("d-123", "Available"),
		},
	}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, region string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "ecs" || resource.Resource != "disk" {
			t.Fatalf("resource = %s/%s, want ecs/disk", resource.Product, resource.Resource)
		}
		if region != "cn-beijing" {
			t.Fatalf("region = %q, want cn-beijing", region)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI("ecs", "disk", "create",
		"--region", "cn-beijing",
		"--zone", "cn-beijing-a",
		"--size", "40",
		"--category", "cloud_essd",
		"--name", "data-1",
	)
	if code != 0 {
		t.Fatalf("ecs disk create exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 2 || fake.calls[0].operation != "CreateDisk" || fake.calls[1].operation != "DescribeDisks" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	createReq := fake.calls[0].request
	for key, want := range map[string]any{
		"RegionId":     "cn-beijing",
		"ZoneId":       "cn-beijing-a",
		"Size":         40,
		"DiskCategory": "cloud_essd",
		"DiskName":     "data-1",
	} {
		if got := createReq[key]; got != want {
			t.Fatalf("CreateDisk request[%s] = %#v, want %#v; request=%#v", key, got, want, createReq)
		}
	}
	disk, _ := decodeObject(t, stdout)["disk"].(map[string]any)
	if disk == nil || disk["id"] != "d-123" || disk["status"] != "Available" {
		t.Fatalf("unexpected output: %s", stdout)
	}
}

func TestECSDiskUpdateRoutesResizeOnly(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{{"RequestId": "req-resize"}}}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "ecs" || resource.Resource != "disk" {
			t.Fatalf("resource = %s/%s, want ecs/disk", resource.Product, resource.Resource)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI("ecs", "disk", "update", "d-123", "--region", "cn-beijing", "--size", "100", "--no-wait")
	if code != 0 {
		t.Fatalf("ecs disk update resize exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "ResizeDisk" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	if fake.calls[0].request["DiskId"] != "d-123" || fake.calls[0].request["NewSize"] != 100 {
		t.Fatalf("ResizeDisk request = %#v", fake.calls[0].request)
	}
}

func TestECSDiskUpdateChargeTypeUsesDiskIDsArray(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{{"RequestId": "req-charge"}}}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "ecs" || resource.Resource != "disk" {
			t.Fatalf("resource = %s/%s, want ecs/disk", resource.Product, resource.Resource)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI("ecs", "disk", "update", "d-123", "--region", "cn-beijing", "--instance", "i-123", "--charge-type", "PrePaid", "--no-wait")
	if code != 0 {
		t.Fatalf("ecs disk update charge type exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "ModifyDiskChargeType" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	if got, ok := fake.calls[0].request["DiskIds"].([]string); !ok || len(got) != 1 || got[0] != "d-123" {
		t.Fatalf("DiskIds = %#v; request=%#v", fake.calls[0].request["DiskIds"], fake.calls[0].request)
	}
	if fake.calls[0].request["InstanceId"] != "i-123" || fake.calls[0].request["DiskChargeType"] != "PrePaid" {
		t.Fatalf("ModifyDiskChargeType request = %#v", fake.calls[0].request)
	}
}

func TestECSDiskUpdateChargeTypeRequiresInstance(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "ecs" || resource.Resource != "disk" {
			t.Fatalf("resource = %s/%s, want ecs/disk", resource.Product, resource.Resource)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI("ecs", "disk", "update", "d-123", "--region", "cn-beijing", "--charge-type", "PrePaid", "--no-wait")
	if code == 0 {
		t.Fatalf("ecs disk update charge type without instance succeeded stdout=%s stderr=%s", stdout, stderr)
	}
	if len(fake.calls) != 0 {
		t.Fatalf("unexpected calls = %#v", fake.calls)
	}
}

func TestECSDiskUpdateAccountEncryptionActionDoesNotRequireID(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{
		{"RequestId": "req-enable"},
		{"RequestId": "req-disable"},
	}}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "ecs" || resource.Resource != "disk" {
			t.Fatalf("resource = %s/%s, want ecs/disk", resource.Product, resource.Resource)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI("ecs", "disk", "update", "--region", "cn-beijing", "--encryption-default", "enable", "--no-wait")
	if code != 0 {
		t.Fatalf("ecs disk update encryption-default enable exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "EnableDiskEncryptionByDefault" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	if _, ok := fake.calls[0].request["DiskId"]; ok {
		t.Fatalf("account-level request must not include DiskId: %#v", fake.calls[0].request)
	}

	stdout, stderr, code = runCLI("ecs", "disk", "update", "--region", "cn-beijing", "--encryption-default", "disable", "--no-wait")
	if code != 0 {
		t.Fatalf("ecs disk update encryption-default disable exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 2 || fake.calls[1].operation != "DisableDiskEncryptionByDefault" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	if _, ok := fake.calls[1].request["DiskId"]; ok {
		t.Fatalf("account-level request must not include DiskId: %#v", fake.calls[1].request)
	}
}

func TestECSDiskUpdateHelpUsesEncryptionDefaultActionFlag(t *testing.T) {
	t.Parallel()
	stdout, stderr, code := runCLI("--lang", "en", "ecs", "disk", "update", "--help")
	if code != 0 {
		t.Fatalf("ecs disk update --help exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if !strings.Contains(stdout, "--encryption-default string") {
		t.Fatalf("update help missing --encryption-default string: %s", stdout)
	}
	for _, removed := range []string{"--enable-encryption-default", "--disable-encryption-default"} {
		if strings.Contains(stdout, removed) {
			t.Fatalf("update help should not expose %s: %s", removed, stdout)
		}
	}
}

func TestECSDiskUpdateAccountEncryptionRejectsDiskMutationMix(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "ecs" || resource.Resource != "disk" {
			t.Fatalf("resource = %s/%s, want ecs/disk", resource.Product, resource.Resource)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI("ecs", "disk", "update", "d-123", "--region", "cn-beijing", "--encryption-default", "enable", "--no-wait")
	if code == 0 {
		t.Fatalf("ecs disk update mixed account/disk mutation succeeded stdout=%s stderr=%s", stdout, stderr)
	}
	if got := errorCode(t, stdout); got != "ConflictingParameters" {
		t.Fatalf("error.code = %q, want ConflictingParameters; stdout=%s", got, stdout)
	}
	if len(fake.calls) != 0 {
		t.Fatalf("conflict should fail before API calls: %#v", fake.calls)
	}
}

func TestECSDiskGetDefaultKMSKeyUsesAccountQuery(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{{"RequestId": "req-kms", "KMSKeyId": "kms-123"}}}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "ecs" || resource.Resource != "disk" {
			t.Fatalf("resource = %s/%s, want ecs/disk", resource.Product, resource.Resource)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI("ecs", "disk", "get", "--region", "cn-beijing", "--default-kms-key")
	if code != 0 {
		t.Fatalf("ecs disk get --default-kms-key exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "DescribeDiskDefaultKMSKeyId" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	defaults, _ := decodeObject(t, stdout)["disk_encryption_default"].(map[string]any)
	if defaults == nil || defaults["kms_key_id"] != "kms-123" {
		t.Fatalf("unexpected output: %s", stdout)
	}
}

func TestECSDiskGetEncryptionDefaultUsesEncryptedField(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{{"RequestId": "req-encryption", "Encrypted": true}}}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "ecs" || resource.Resource != "disk" {
			t.Fatalf("resource = %s/%s, want ecs/disk", resource.Product, resource.Resource)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI("ecs", "disk", "get", "--region", "cn-beijing", "--encryption-default")
	if code != 0 {
		t.Fatalf("ecs disk get --encryption-default exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "DescribeDiskEncryptionByDefaultStatus" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	defaults, _ := decodeObject(t, stdout)["disk_encryption_default"].(map[string]any)
	if defaults == nil || defaults["enabled"] != true {
		t.Fatalf("unexpected output: %s", stdout)
	}
}

func TestECSDiskGetByIDDoesNotEmitEncryptionDefault(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{fakeDiskListResponse("d-123", "Available")}}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "ecs" || resource.Resource != "disk" {
			t.Fatalf("resource = %s/%s, want ecs/disk", resource.Product, resource.Resource)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI("ecs", "disk", "get", "d-123", "--region", "cn-beijing")
	if code != 0 {
		t.Fatalf("ecs disk get by ID exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "DescribeDisks" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	out := decodeObject(t, stdout)
	if _, ok := out["disk_encryption_default"]; ok {
		t.Fatalf("ordinary disk get must not emit disk_encryption_default: %s", stdout)
	}
}

func TestECSDiskAttachUsesDesignedAPI(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{
		responses: []map[string]any{
			{"RequestId": "req-attach"},
			fakeDiskListResponse("d-123", "In_use"),
		},
	}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "ecs" || resource.Resource != "disk" {
			t.Fatalf("resource = %s/%s, want ecs/disk", resource.Product, resource.Resource)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI("ecs", "disk", "attach", "d-123", "--region", "cn-beijing", "--instance", "i-123", "--delete-with-instance")
	if code != 0 {
		t.Fatalf("ecs disk attach exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 2 || fake.calls[0].operation != "AttachDisk" || fake.calls[1].operation != "DescribeDisks" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	if fake.calls[0].request["DiskId"] != "d-123" || fake.calls[0].request["InstanceId"] != "i-123" || fake.calls[0].request["DeleteWithInstance"] != true {
		t.Fatalf("AttachDisk request = %#v", fake.calls[0].request)
	}
}

func TestECSDiskDetachWaiterIgnoresInstanceFilter(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{
		responses: []map[string]any{
			{"RequestId": "req-detach"},
			fakeDiskListResponse("d-123", "Available"),
		},
	}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "ecs" || resource.Resource != "disk" {
			t.Fatalf("resource = %s/%s, want ecs/disk", resource.Product, resource.Resource)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI("ecs", "disk", "detach", "d-123", "--region", "cn-beijing", "--instance", "i-123")
	if code != 0 {
		t.Fatalf("ecs disk detach exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 2 || fake.calls[0].operation != "DetachDisk" || fake.calls[1].operation != "DescribeDisks" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	if fake.calls[0].request["InstanceId"] != "i-123" {
		t.Fatalf("DetachDisk request = %#v", fake.calls[0].request)
	}
	for _, call := range fake.calls[1:] {
		if _, ok := call.request["InstanceId"]; ok {
			t.Fatalf("detach DescribeDisks must not keep InstanceId filter: %#v", call.request)
		}
		if got, ok := call.request["DiskIds"].([]string); !ok || len(got) != 1 || got[0] != "d-123" {
			t.Fatalf("detach DescribeDisks must still query requested disk id: %#v", call.request)
		}
	}
}

func TestECSDiskDeleteRetriesHiddenInitializingStatus(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{
		errors: []error{
			ecerrors.Service("CloudAPIError", "disk is still initializing", false,
				ecerrors.WithRawCause("IncorrectDiskStatus.Initializing", "The current disk status does not support this operation.")),
		},
		responses: []map[string]any{
			{"RequestId": "req-delete"},
			{
				"RequestId":  "req-describe",
				"TotalCount": 0,
				"Disks":      map[string]any{"Disk": []any{}},
			},
		},
	}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "ecs" || resource.Resource != "disk" {
			t.Fatalf("resource = %s/%s, want ecs/disk", resource.Product, resource.Resource)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI("ecs", "disk", "delete", "d-123", "--region", "cn-beijing")
	if code != 0 {
		t.Fatalf("ecs disk delete exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 3 || fake.calls[0].operation != "DeleteDisk" || fake.calls[1].operation != "DeleteDisk" || fake.calls[2].operation != "DescribeDisks" {
		t.Fatalf("calls = %#v", fake.calls)
	}
}

func TestECSDiskCloneUsesSupportedAPIParameters(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{
		{"RequestId": "req-clone", "TaskGroupId": "tg-123"},
		{"RequestId": "req-task", "TaskSet": map[string]any{"Task": []any{
			map[string]any{"TaskId": "t-123", "TaskStatus": "Finished", "TaskAction": "CloneDisks", "ResourceId": "d-copy"},
		}}},
	}}
	runCLI := withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "ecs" || resource.Resource != "disk" {
			t.Fatalf("resource = %s/%s, want ecs/disk", resource.Product, resource.Resource)
		}
		return fake, nil
	})

	stdout, stderr, code := runCLI("ecs", "disk", "clone", "d-123",
		"--region", "cn-beijing",
		"--size", "80",
		"--category", "cloud_essd",
		"--performance-level", "PL1",
		"--multi-attach", "Disabled",
		"--name", "copy-1",
		"--encrypted",
		"--kms-key-id", "kms-123",
	)
	if code != 0 {
		t.Fatalf("ecs disk clone exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 2 || fake.calls[0].operation != "CloneDisks" || fake.calls[1].operation != "DescribeTasks" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	request := fake.calls[0].request
	for key, want := range map[string]any{
		"RegionId":         "cn-beijing",
		"SourceDiskId":     "d-123",
		"Size":             80,
		"DiskCategory":     "cloud_essd",
		"PerformanceLevel": "PL1",
		"MultiAttach":      "Disabled",
		"DiskName":         "copy-1",
		"Encrypted":        true,
		"KmsKeyId":         "kms-123",
	} {
		if got := request[key]; got != want {
			t.Fatalf("CloneDisks request[%s] = %#v, want %#v; request=%#v", key, got, want, request)
		}
	}
	for _, key := range []string{"Description", "DestinationZoneId", "Amount"} {
		if _, ok := request[key]; ok {
			t.Fatalf("CloneDisks must not receive %s: %#v", key, request)
		}
	}
	if fake.calls[1].request["TaskGroupId"] != "tg-123" {
		t.Fatalf("DescribeTasks request = %#v", fake.calls[1].request)
	}
	out := decodeObject(t, stdout)
	disk, _ := out["disk"].(map[string]any)
	if disk == nil || disk["task_group_id"] != "tg-123" || disk["source_disk"] != "d-123" {
		t.Fatalf("unexpected output: %s", stdout)
	}
}

package spec_resource

import (
	"testing"

	"ecctl/pkg/engine"
	"ecctl/pkg/spec"
)

func fakeImageListResponse(id string, status string) map[string]any {
	return map[string]any{
		"RequestId":  "req-describe-images",
		"TotalCount": 1,
		"Images": map[string]any{"Image": []any{
			map[string]any{
				"ImageId":         id,
				"ImageName":       "web-snapshot",
				"Status":          status,
				"RegionId":        "cn-beijing",
				"Architecture":    "x86_64",
				"OSType":          "linux",
				"Platform":        "Ubuntu",
				"Size":            float64(40),
				"ImageOwnerAlias": "self",
				"BootMode":        "UEFI",
				"ImageFamily":     "web",
				"Progress":        "100%",
				"ResourceGroupId": "rg-1",
			},
		}},
	}
}

func imageCaller(t *testing.T, fake *fakeSpecCaller) func(args ...string) (string, string, int) {
	t.Helper()
	return withCaller(func(_ string, _ string, resource spec.ResourceSpec, _ string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "ecs" || resource.Resource != "image" {
			t.Fatalf("resource = %s/%s, want ecs/image", resource.Product, resource.Resource)
		}
		return fake, nil
	})
}

func TestECSImageCreateInvokesCreateImageAndReadsState(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{
		responses: []map[string]any{
			{"RequestId": "req-create", "ImageId": "m-bp1abc"},
			fakeImageListResponse("m-bp1abc", "Available"),
		},
	}
	runCLI := imageCaller(t, fake)

	stdout, stderr, code := runCLI(
		"ecs", "image", "create",
		"--region", "cn-beijing",
		"--instance", "i-123",
		"--name", "web-snapshot",
		"--image-family", "web",
		"--no-wait",
	)
	if code != 0 {
		t.Fatalf("ecs image create exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	// With --no-wait, the wait probe is skipped — only CreateImage runs.
	if len(fake.calls) != 1 || fake.calls[0].operation != "CreateImage" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	request := fake.calls[0].request
	for key, want := range map[string]any{
		"RegionId":    "cn-beijing",
		"InstanceId":  "i-123",
		"ImageName":   "web-snapshot",
		"ImageFamily": "web",
	} {
		if got := request[key]; got != want {
			t.Fatalf("CreateImage request[%s] = %#v, want %#v; request=%#v", key, got, want, request)
		}
	}
	if _, ok := request["ClientToken"]; !ok {
		t.Fatalf("CreateImage must receive ClientToken: %#v", request)
	}
	image, _ := decodeObject(t, stdout)["image"].(map[string]any)
	if image == nil || image["id"] != "m-bp1abc" {
		t.Fatalf("unexpected output: %s", stdout)
	}
}

func TestECSImageUpdateRoutesAttributesOnly(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{{"RequestId": "req-update"}}}
	runCLI := imageCaller(t, fake)

	stdout, stderr, code := runCLI(
		"ecs", "image", "update", "m-bp1abc",
		"--region", "cn-beijing",
		"--name", "new-name",
		"--description", "polished",
		"--no-wait",
	)
	if code != 0 {
		t.Fatalf("ecs image update exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "ModifyImageAttribute" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	request := fake.calls[0].request
	if request["ImageId"] != "m-bp1abc" || request["ImageName"] != "new-name" || request["Description"] != "polished" {
		t.Fatalf("ModifyImageAttribute request = %#v", request)
	}
}

func TestECSImageUpdateSharePermissionRoutesShareAPI(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{{"RequestId": "req-share"}}}
	runCLI := imageCaller(t, fake)

	stdout, stderr, code := runCLI(
		"ecs", "image", "update", "m-bp1abc",
		"--region", "cn-beijing",
		"--share-add", "1111,2222",
		"--no-wait",
	)
	if code != 0 {
		t.Fatalf("ecs image update share exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "ModifyImageSharePermission" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	request := fake.calls[0].request
	if request["ImageId"] != "m-bp1abc" {
		t.Fatalf("ModifyImageSharePermission request = %#v", request)
	}
	// Either AddAccount or AddAccount.N — confirm at least one indexed slot received a value.
	found := false
	for k, v := range request {
		if len(k) >= len("AddAccount") && k[:len("AddAccount")] == "AddAccount" && v == "1111" {
			found = true
		}
	}
	if !found {
		t.Fatalf("ModifyImageSharePermission must carry account 1111 under AddAccount.N: %#v", request)
	}
}

func TestECSImageDeleteForceIsOptIn(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{{"RequestId": "req-delete"}}}
	runCLI := imageCaller(t, fake)

	stdout, stderr, code := runCLI("ecs", "image", "delete", "m-bp1abc", "--region", "cn-beijing", "--no-wait")
	if code != 0 {
		t.Fatalf("ecs image delete exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "DeleteImage" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	request := fake.calls[0].request
	if got, ok := request["Force"]; ok && got == true {
		t.Fatalf("DeleteImage Force must default to false; request=%#v", request)
	}
}

func TestECSImageCopyRoutesCancelToCancelCopyImage(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{{"RequestId": "req-cancel"}}}
	runCLI := imageCaller(t, fake)

	stdout, stderr, code := runCLI("ecs", "image", "copy", "m-bp1abc", "--region", "cn-beijing", "--cancel")
	if code != 0 {
		t.Fatalf("ecs image copy --cancel exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "CancelCopyImage" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	if fake.calls[0].request["ImageId"] != "m-bp1abc" {
		t.Fatalf("CancelCopyImage request = %#v", fake.calls[0].request)
	}
}

func TestECSImageCopyRequiresDestinationRegion(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{}
	runCLI := imageCaller(t, fake)

	stdout, stderr, code := runCLI("ecs", "image", "copy", "m-bp1abc", "--region", "cn-beijing", "--destination-name", "copy-1")
	if code == 0 {
		t.Fatalf("ecs image copy without --destination-region should fail; stdout=%s stderr=%s", stdout, stderr)
	}
	if len(fake.calls) != 0 {
		t.Fatalf("validation should fail before API: %#v", fake.calls)
	}
}

func TestECSImageCopyRejectsCancelMixedWithDestination(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{}
	runCLI := imageCaller(t, fake)

	stdout, stderr, code := runCLI(
		"ecs", "image", "copy", "m-bp1abc",
		"--region", "cn-beijing",
		"--cancel",
		"--destination-region", "cn-hangzhou",
	)
	if code == 0 {
		t.Fatalf("--cancel with --destination-region should be rejected; stdout=%s stderr=%s", stdout, stderr)
	}
	if got := errorCode(t, stdout); got != "ConflictingParameters" {
		t.Fatalf("error.code = %q, want ConflictingParameters; stdout=%s", got, stdout)
	}
	if len(fake.calls) != 0 {
		t.Fatalf("conflict should fail before API calls: %#v", fake.calls)
	}
}

func TestECSImageCopyWaitsForDestinationImage(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{
		{"RequestId": "req-copy", "ImageId": "m-copy"},
		fakeImageListResponse("m-copy", "Available"),
	}}
	runCLI := imageCaller(t, fake)

	stdout, stderr, code := runCLI(
		"ecs", "image", "copy", "m-bp1abc",
		"--region", "cn-beijing",
		"--destination-region", "cn-hangzhou",
		"--destination-name", "copy-1",
	)
	if code != 0 {
		t.Fatalf("ecs image copy exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 2 || fake.calls[0].operation != "CopyImage" || fake.calls[1].operation != "DescribeImages" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	readback := fake.calls[1].request
	if readback["RegionId"] != "cn-hangzhou" || !sliceContainsValues(readback["ImageId"], "m-copy") {
		t.Fatalf("DescribeImages destination readback request = %#v", readback)
	}
	image, _ := decodeObject(t, stdout)["image"].(map[string]any)
	if image == nil || image["id"] != "m-copy" || image["status"] != "Available" {
		t.Fatalf("unexpected copy output: %s", stdout)
	}
}

func TestECSImageExportInvokesExportImage(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{
		{"RequestId": "req-export", "TaskId": "t-export-1"},
		{"RequestId": "req-task", "TaskId": "t-export-1", "TaskStatus": "Finished", "TaskAction": "ExportImage"},
	}}
	runCLI := imageCaller(t, fake)

	stdout, stderr, code := runCLI(
		"ecs", "image", "export", "m-bp1abc",
		"--region", "cn-beijing",
		"--oss-bucket", "snapshots",
		"--oss-prefix", "linux/",
	)
	if code != 0 {
		t.Fatalf("ecs image export exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 2 || fake.calls[0].operation != "ExportImage" || fake.calls[1].operation != "DescribeTaskAttribute" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	request := fake.calls[0].request
	if request["ImageId"] != "m-bp1abc" || request["OSSBucket"] != "snapshots" || request["OSSPrefix"] != "linux/" {
		t.Fatalf("ExportImage request = %#v", request)
	}
	if fake.calls[1].request["TaskId"] != "t-export-1" {
		t.Fatalf("DescribeTaskAttribute request = %#v", fake.calls[1].request)
	}
	out := decodeObject(t, stdout)
	if out["task_id"] != "t-export-1" && out["image"] == nil {
		t.Fatalf("export output missing task_id: %s", stdout)
	}
}

func TestECSImageListProbesDescribeImagesWithFilters(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{fakeImageListResponse("m-bp1abc", "Available")}}
	runCLI := imageCaller(t, fake)

	stdout, stderr, code := runCLI(
		"ecs", "image", "list",
		"--region", "cn-beijing",
		"--filter", "owner-alias=self",
		"--filter", "status=Available",
	)
	if code != 0 {
		t.Fatalf("ecs image list exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "DescribeImages" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	request := fake.calls[0].request
	if request["ImageOwnerAlias"] != "self" || request["Status"] != "Available" {
		t.Fatalf("DescribeImages filters not propagated: %#v", request)
	}
	out := decodeObject(t, stdout)
	images, _ := out["images"].([]any)
	if len(images) != 1 {
		t.Fatalf("images = %#v; stdout=%s", images, stdout)
	}
}

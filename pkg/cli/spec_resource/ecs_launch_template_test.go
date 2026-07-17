package spec_resource

import (
	"strings"
	"testing"
)

func fakeLaunchTemplateListResponse(id, name string, defaultVersion int64) map[string]any {
	return map[string]any{
		"RequestId":  "req-describe-templates",
		"TotalCount": 1,
		"LaunchTemplateSets": map[string]any{"LaunchTemplateSet": []any{
			map[string]any{
				"LaunchTemplateId":     id,
				"LaunchTemplateName":   name,
				"DefaultVersionNumber": defaultVersion,
				"LatestVersionNumber":  defaultVersion,
			},
		}},
	}
}

func fakeLaunchTemplateVersionsResponse(id, name string, version int64) map[string]any {
	return map[string]any{
		"RequestId":  "req-describe-template-versions",
		"TotalCount": 1,
		"LaunchTemplateVersionSets": map[string]any{"LaunchTemplateVersionSet": []any{
			map[string]any{
				"LaunchTemplateId":   id,
				"LaunchTemplateName": name,
				"VersionNumber":      version,
				"DefaultVersion":     true,
				"LaunchTemplateData": map[string]any{
					"ImageId":          "aliyun_3_x64_20G_alibase_20240528.vhd",
					"InstanceType":     "ecs.g6.large",
					"SecurityGroupIds": map[string]any{"SecurityGroupId": []any{"sg-1"}},
					"VSwitchId":        "vsw-1",
				},
			},
		}},
	}
}

func TestECSLaunchTemplateSchemaRegistration(t *testing.T) {
	t.Parallel()

	stdout, stderr, code := runCLI("schema", "ecs.launch-template.create")
	if code != 0 {
		t.Fatalf("schema ecs.launch-template.create exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	for _, want := range []string{`"name"`, `"image"`, `"security-groups"`, `"api-param"`} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("schema missing %s: %s", want, stdout)
		}
	}

	stdout, stderr, code = runCLI("ecs", "lt", "create", "--help")
	if code != 0 {
		t.Fatalf("ecs lt create --help exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if !strings.Contains(stdout, "Create launch template") {
		t.Fatalf("alias help did not resolve launch-template command: %s", stdout)
	}
}

func TestECSLaunchTemplateCreateUsesCreateLaunchTemplate(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{
		{"RequestId": "req-create", "LaunchTemplateId": "lt-1", "LaunchTemplateVersionNumber": int64(1)},
	}}
	runCLI := catalogCaller(t, "ecs", "launch-template", fake)

	stdout, stderr, code := runCLI(
		"ecs", "launch-template", "create",
		"--region", "cn-hangzhou",
		"--name", "web-lt",
		"--image", "aliyun_3_x64_20G_alibase_20240528.vhd",
		"--type", "ecs.g6.large",
		"--sg", "sg-1",
		"--vswitch", "vsw-1",
		"--keypair", "web-key",
		"--no-wait",
	)
	if code != 0 {
		t.Fatalf("ecs launch-template create exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "CreateLaunchTemplate" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	req := fake.calls[0].request
	want := map[string]any{
		"LaunchTemplateName": "web-lt",
		"ImageId":            "aliyun_3_x64_20G_alibase_20240528.vhd",
		"InstanceType":       "ecs.g6.large",
		"SecurityGroupId":    "sg-1",
		"VSwitchId":          "vsw-1",
		"KeyPairName":        "web-key",
	}
	for key, value := range want {
		if req[key] != value {
			t.Fatalf("%s = %#v, want %#v; request=%#v", key, req[key], value, req)
		}
	}
}

func TestECSLaunchTemplateCreateUsesRenamedTagAndResourceGroupFlags(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{
		{"RequestId": "req-create", "LaunchTemplateId": "lt-1", "LaunchTemplateVersionNumber": int64(1)},
	}}
	runCLI := catalogCaller(t, "ecs", "launch-template", fake)

	stdout, stderr, code := runCLI(
		"ecs", "launch-template", "create",
		"--region", "cn-hangzhou",
		"--name", "web-lt",
		"--resource-resource-group", "rg-instance",
		"--resource-tag", "env=prod",
		"--resource-group", "rg-template",
		"--tag", "owner=platform",
		"--no-wait",
	)
	if code != 0 {
		t.Fatalf("ecs launch-template create renamed flags exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	req := fake.calls[0].request
	if req["ResourceGroupId"] != "rg-instance" || req["TemplateResourceGroupId"] != "rg-template" {
		t.Fatalf("resource group request = %#v", req)
	}
	if !sliceContainsValues(req["Tag"], "env=prod") || !sliceContainsValues(req["TemplateTag"], "owner=platform") {
		t.Fatalf("tag request = %#v", req)
	}
}

func TestECSLaunchTemplateCreateHelpUsesRenamedTemplateAndResourceFlags(t *testing.T) {
	t.Parallel()

	stdout, stderr, code := runCLI("--lang", "en", "ecs", "launch-template", "create", "--help")
	if code != 0 {
		t.Fatalf("ecs launch-template create --help exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	for _, want := range []string{"--resource-resource-group", "--resource-tag", "--resource-group", "--tag"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("create help missing %s: %s", want, stdout)
		}
	}
	for _, forbidden := range []string{"--template-resource-group", "--template-tag"} {
		if strings.Contains(stdout, forbidden) {
			t.Fatalf("create help should not expose %s: %s", forbidden, stdout)
		}
	}
}

func TestECSLaunchTemplateCreateWaitsForTemplateByDefault(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{
		{"RequestId": "req-create", "LaunchTemplateId": "lt-1", "LaunchTemplateVersionNumber": int64(1)},
		fakeLaunchTemplateListResponse("lt-1", "web-lt", 1),
		fakeLaunchTemplateListResponse("lt-1", "web-lt", 1),
		fakeLaunchTemplateVersionsResponse("lt-1", "web-lt", 1),
	}}
	runCLI := catalogCaller(t, "ecs", "launch-template", fake)

	stdout, stderr, code := runCLI(
		"ecs", "launch-template", "create",
		"--region", "cn-hangzhou",
		"--name", "web-lt",
	)
	if code != 0 {
		t.Fatalf("ecs launch-template create exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 4 ||
		fake.calls[0].operation != "CreateLaunchTemplate" ||
		fake.calls[1].operation != "DescribeLaunchTemplates" ||
		fake.calls[2].operation != "DescribeLaunchTemplates" ||
		fake.calls[3].operation != "DescribeLaunchTemplateVersions" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	if fake.calls[1].request["LaunchTemplateId.1"] != "lt-1" || fake.calls[2].request["LaunchTemplateId.1"] != "lt-1" {
		t.Fatalf("create readback requests = %#v %#v", fake.calls[1].request, fake.calls[2].request)
	}
}

func TestECSLaunchTemplateUpdateCreateVersionRoutesAndReadsBack(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{
		{"RequestId": "req-create-version", "LaunchTemplateId": "lt-1", "LaunchTemplateVersionNumber": int64(2)},
		fakeLaunchTemplateVersionsResponse("lt-1", "web-lt", 2),
		fakeLaunchTemplateListResponse("lt-1", "web-lt", 1),
		fakeLaunchTemplateVersionsResponse("lt-1", "web-lt", 2),
	}}
	runCLI := catalogCaller(t, "ecs", "launch-template", fake)

	stdout, stderr, code := runCLI(
		"ecs", "launch-template", "update", "lt-1",
		"--region", "cn-hangzhou",
		"--create-version",
		"--version-description", "rollout",
		"--image", "aliyun_3_x64_20G_alibase_20240528.vhd",
		"--type", "ecs.g6.large",
	)
	if code != 0 {
		t.Fatalf("ecs launch-template update --create-version exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 4 ||
		fake.calls[0].operation != "CreateLaunchTemplateVersion" ||
		fake.calls[1].operation != "DescribeLaunchTemplateVersions" ||
		fake.calls[2].operation != "DescribeLaunchTemplates" ||
		fake.calls[3].operation != "DescribeLaunchTemplateVersions" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	req := fake.calls[0].request
	if req["LaunchTemplateId"] != "lt-1" || req["VersionDescription"] != "rollout" || req["ImageId"] != "aliyun_3_x64_20G_alibase_20240528.vhd" {
		t.Fatalf("CreateLaunchTemplateVersion request = %#v", req)
	}
	if fake.calls[1].request["LaunchTemplateVersion.1"] != int64(2) {
		t.Fatalf("DescribeLaunchTemplateVersions should wait for created version: %#v", fake.calls[1].request)
	}
	if fake.calls[3].request["LaunchTemplateId"] != "lt-1" || fake.calls[3].request["LaunchTemplateVersion.1"] != int64(2) {
		t.Fatalf("DescribeLaunchTemplateVersions readback request = %#v", fake.calls[3].request)
	}
}

func TestECSLaunchTemplateUpdateCreateVersionFailsWhenCreatedVersionNotVisible(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{
		{"RequestId": "req-create-version", "LaunchTemplateId": "lt-1", "LaunchTemplateVersionNumber": int64(2)},
		fakeLaunchTemplateListResponse("lt-1", "web-lt", 1),
		{"RequestId": "req-describe-template-versions", "TotalCount": 0, "LaunchTemplateVersionSets": map[string]any{"LaunchTemplateVersionSet": []any{}}},
	}}
	runCLI := catalogCaller(t, "ecs", "launch-template", fake)

	stdout, stderr, code := runCLI(
		"ecs", "launch-template", "update", "lt-1",
		"--region", "cn-hangzhou",
		"--create-version",
		"--image", "aliyun_3_x64_20G_alibase_20240528.vhd",
		"--timeout", "1ms",
	)
	if code == 0 {
		t.Fatalf("ecs launch-template update --create-version should fail when version is not visible; stderr=%s stdout=%s", stderr, stdout)
	}
}

func TestECSLaunchTemplateUpdateDefaultVersionRoutesAndReadsBack(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{
		{"RequestId": "req-default-version"},
		fakeLaunchTemplateVersionsResponse("lt-1", "web-lt", 2),
		fakeLaunchTemplateListResponse("lt-1", "web-lt", 2),
		fakeLaunchTemplateVersionsResponse("lt-1", "web-lt", 2),
	}}
	runCLI := catalogCaller(t, "ecs", "launch-template", fake)

	stdout, stderr, code := runCLI("ecs", "launch-template", "update", "lt-1", "--region", "cn-hangzhou", "--default-version", "2")
	if code != 0 {
		t.Fatalf("ecs launch-template update --default-version exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 4 ||
		fake.calls[0].operation != "ModifyLaunchTemplateDefaultVersion" ||
		fake.calls[1].operation != "DescribeLaunchTemplateVersions" ||
		fake.calls[2].operation != "DescribeLaunchTemplates" ||
		fake.calls[3].operation != "DescribeLaunchTemplateVersions" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	req := fake.calls[0].request
	if req["LaunchTemplateId"] != "lt-1" || req["DefaultVersionNumber"] != 2 {
		t.Fatalf("ModifyLaunchTemplateDefaultVersion request = %#v", req)
	}
	if fake.calls[1].request["DefaultVersion"] != true || fake.calls[1].request["LaunchTemplateVersion.1"] != 2 {
		t.Fatalf("DescribeLaunchTemplateVersions should wait for target default version: %#v", fake.calls[1].request)
	}
	if fake.calls[3].request["DefaultVersion"] != true || fake.calls[3].request["LaunchTemplateVersion.1"] != 2 {
		t.Fatalf("DescribeLaunchTemplateVersions should read default version: %#v", fake.calls[3].request)
	}
	template, _ := decodeObject(t, stdout)["launch_template"].(map[string]any)
	if _, ok := template["version"]; ok {
		t.Fatalf("default version readback should not expose raw version object: %s", stdout)
	}
}

func TestECSLaunchTemplateUpdateDefaultVersionFailsWhenVersionNotVisible(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{
		{"RequestId": "req-default-version"},
		{"RequestId": "req-describe-template-versions", "TotalCount": 0, "LaunchTemplateVersionSets": map[string]any{"LaunchTemplateVersionSet": []any{}}},
	}}
	runCLI := catalogCaller(t, "ecs", "launch-template", fake)

	stdout, stderr, code := runCLI("ecs", "launch-template", "update", "lt-1", "--region", "cn-hangzhou", "--default-version", "2", "--timeout", "1ms")
	if code == 0 {
		t.Fatalf("ecs launch-template update --default-version should fail when version is not visible; stderr=%s stdout=%s", stderr, stdout)
	}
}

func TestECSLaunchTemplateDeleteVersionRoutesDeleteLaunchTemplateVersion(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{
		fakeLaunchTemplateListResponse("lt-1", "web-lt", 1),
		{"RequestId": "req-delete-version"},
	}}
	runCLI := catalogCaller(t, "ecs", "launch-template", fake)

	stdout, stderr, code := runCLI("ecs", "launch-template", "delete", "lt-1", "--region", "cn-hangzhou", "--version", "2")
	if code != 0 {
		t.Fatalf("ecs launch-template delete --version exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 2 || fake.calls[0].operation != "DescribeLaunchTemplates" || fake.calls[1].operation != "DeleteLaunchTemplateVersion" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	req := fake.calls[1].request
	if req["LaunchTemplateId"] != "lt-1" || req["DeleteVersion.1"] != 2 {
		t.Fatalf("DeleteLaunchTemplateVersion request = %#v", req)
	}
}

func TestECSLaunchTemplateDeleteHelpOmitsNameFlag(t *testing.T) {
	t.Parallel()

	stdout, stderr, code := runCLI("--lang", "en", "ecs", "launch-template", "delete", "--help")
	if code != 0 {
		t.Fatalf("ecs launch-template delete --help exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if strings.Contains(stdout, "--name") {
		t.Fatalf("delete help should not expose --name: %s", stdout)
	}
}

func TestECSLaunchTemplateDeleteByPrefixedTargetFallsBackToNameWhenIDMissing(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{
		{"RequestId": "req-missing-id", "TotalCount": 0, "LaunchTemplateSets": map[string]any{"LaunchTemplateSet": []any{}}},
		fakeLaunchTemplateListResponse("lt-1", "lt-web", 1),
		{"RequestId": "req-delete"},
	}}
	runCLI := catalogCaller(t, "ecs", "launch-template", fake)

	stdout, stderr, code := runCLI("ecs", "launch-template", "delete", "--region", "cn-hangzhou", "lt-web")
	if code != 0 {
		t.Fatalf("ecs launch-template delete fallback name exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 3 ||
		fake.calls[0].operation != "DescribeLaunchTemplates" ||
		fake.calls[1].operation != "DescribeLaunchTemplates" ||
		fake.calls[2].operation != "DeleteLaunchTemplate" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	if fake.calls[0].request["LaunchTemplateId.1"] != "lt-web" {
		t.Fatalf("first lookup should use id for lt- target: %#v", fake.calls[0].request)
	}
	if fake.calls[1].request["LaunchTemplateName.1"] != "lt-web" {
		t.Fatalf("fallback lookup should use name for lt- target: %#v", fake.calls[1].request)
	}
	if fake.calls[2].request["LaunchTemplateId"] != "lt-1" {
		t.Fatalf("delete should use resolved template: %#v", fake.calls[2].request)
	}
}

func TestECSLaunchTemplateGetWithVersionsAddsVersionProbe(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{
		fakeLaunchTemplateListResponse("lt-1", "web-lt", 1),
		fakeLaunchTemplateVersionsResponse("lt-1", "web-lt", 1),
	}}
	runCLI := catalogCaller(t, "ecs", "launch-template", fake)

	stdout, stderr, code := runCLI("ecs", "launch-template", "get", "lt-1", "--region", "cn-hangzhou", "--with-versions")
	if code != 0 {
		t.Fatalf("ecs launch-template get --with-versions exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 2 || fake.calls[0].operation != "DescribeLaunchTemplates" || fake.calls[1].operation != "DescribeLaunchTemplateVersions" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	if fake.calls[1].request["LaunchTemplateId"] != "lt-1" {
		t.Fatalf("DescribeLaunchTemplateVersions request = %#v", fake.calls[1].request)
	}
	out := decodeObject(t, stdout)
	template, _ := out["launch_template"].(map[string]any)
	if template == nil || template["id"] != "lt-1" {
		t.Fatalf("unexpected get output: %s", stdout)
	}
	versions, _ := template["versions"].([]any)
	if len(versions) != 1 {
		t.Fatalf("versions = %#v; stdout=%s", template["versions"], stdout)
	}
}

func TestECSLaunchTemplateGetHelpOmitsNameFlag(t *testing.T) {
	t.Parallel()

	stdout, stderr, code := runCLI("--lang", "en", "ecs", "launch-template", "get", "--help")
	if code != 0 {
		t.Fatalf("ecs launch-template get --help exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if strings.Contains(stdout, "--name") {
		t.Fatalf("get help should not expose --name: %s", stdout)
	}
}

func TestECSLaunchTemplateGetVersionAddsVersionProbe(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{
		fakeLaunchTemplateListResponse("lt-1", "web-lt", 1),
		fakeLaunchTemplateVersionsResponse("lt-1", "web-lt", 2),
	}}
	runCLI := catalogCaller(t, "ecs", "launch-template", fake)

	stdout, stderr, code := runCLI("ecs", "launch-template", "get", "lt-1", "--region", "cn-hangzhou", "--version", "2")
	if code != 0 {
		t.Fatalf("ecs launch-template get --version exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 2 || fake.calls[0].operation != "DescribeLaunchTemplates" || fake.calls[1].operation != "DescribeLaunchTemplateVersions" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	if fake.calls[1].request["LaunchTemplateId"] != "lt-1" || fake.calls[1].request["LaunchTemplateVersion.1"] != 2 {
		t.Fatalf("DescribeLaunchTemplateVersions request = %#v", fake.calls[1].request)
	}
	template, _ := decodeObject(t, stdout)["launch_template"].(map[string]any)
	versions, _ := template["versions"].([]any)
	if len(versions) != 1 {
		t.Fatalf("versions = %#v; stdout=%s", template["versions"], stdout)
	}
}

func TestECSLaunchTemplateGetVersionFailsWhenVersionNotVisible(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{
		fakeLaunchTemplateListResponse("lt-1", "web-lt", 1),
		{"RequestId": "req-describe-template-versions", "TotalCount": 0, "LaunchTemplateVersionSets": map[string]any{"LaunchTemplateVersionSet": []any{}}},
	}}
	runCLI := catalogCaller(t, "ecs", "launch-template", fake)

	stdout, stderr, code := runCLI("ecs", "launch-template", "get", "lt-1", "--region", "cn-hangzhou", "--version", "2")
	if code == 0 {
		t.Fatalf("ecs launch-template get --version should fail when version is not visible; stderr=%s stdout=%s", stderr, stdout)
	}
}

func TestECSLaunchTemplateGetByNameFiltersDescribeLaunchTemplates(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{
		fakeLaunchTemplateListResponse("lt-1", "web-lt", 1),
	}}
	runCLI := catalogCaller(t, "ecs", "launch-template", fake)

	stdout, stderr, code := runCLI("ecs", "launch-template", "get", "--region", "cn-hangzhou", "web-lt")
	if code != 0 {
		t.Fatalf("ecs launch-template get by name exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "DescribeLaunchTemplates" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	if fake.calls[0].request["LaunchTemplateName.1"] != "web-lt" {
		t.Fatalf("DescribeLaunchTemplates name lookup request = %#v", fake.calls[0].request)
	}
}

func TestECSLaunchTemplateGetByPrefixedTargetFallsBackToNameWhenIDMissing(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{
		{"RequestId": "req-missing-id", "TotalCount": 0, "LaunchTemplateSets": map[string]any{"LaunchTemplateSet": []any{}}},
		fakeLaunchTemplateListResponse("lt-1", "lt-web", 1),
	}}
	runCLI := catalogCaller(t, "ecs", "launch-template", fake)

	stdout, stderr, code := runCLI("ecs", "launch-template", "get", "--region", "cn-hangzhou", "lt-web")
	if code != 0 {
		t.Fatalf("ecs launch-template get fallback name exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 2 ||
		fake.calls[0].operation != "DescribeLaunchTemplates" ||
		fake.calls[1].operation != "DescribeLaunchTemplates" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	if fake.calls[0].request["LaunchTemplateId.1"] != "lt-web" {
		t.Fatalf("first lookup should use id for lt- target: %#v", fake.calls[0].request)
	}
	if fake.calls[1].request["LaunchTemplateName.1"] != "lt-web" {
		t.Fatalf("fallback lookup should use name for lt- target: %#v", fake.calls[1].request)
	}
}

func TestECSLaunchTemplateGetByNameWithVersionsUsesResolvedTemplate(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{
		fakeLaunchTemplateListResponse("lt-1", "web-lt", 1),
		fakeLaunchTemplateVersionsResponse("lt-1", "web-lt", 1),
	}}
	runCLI := catalogCaller(t, "ecs", "launch-template", fake)

	stdout, stderr, code := runCLI("ecs", "launch-template", "get", "--region", "cn-hangzhou", "web-lt", "--with-versions")
	if code != 0 {
		t.Fatalf("ecs launch-template get by name --with-versions exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 2 ||
		fake.calls[0].operation != "DescribeLaunchTemplates" ||
		fake.calls[1].operation != "DescribeLaunchTemplateVersions" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	if fake.calls[1].request["LaunchTemplateId"] != "lt-1" || fake.calls[1].request["LaunchTemplateName"] != "web-lt" {
		t.Fatalf("versions lookup should use resolved template: %#v", fake.calls[1].request)
	}
}

func TestECSLaunchTemplateListHelpOmitsNameAndTemplateFilterFlags(t *testing.T) {
	t.Parallel()

	stdout, stderr, code := runCLI("--lang", "en", "ecs", "launch-template", "list", "--help")
	if code != 0 {
		t.Fatalf("ecs launch-template list --help exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	for _, forbidden := range []string{"--name", "--names", "--template-resource-group", "--template-tag"} {
		if strings.Contains(stdout, forbidden) {
			t.Fatalf("list help should not expose %s: %s", forbidden, stdout)
		}
	}
	for _, want := range []string{"--filter", "resource-group", "tag.<key>"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("list help missing %s: %s", want, stdout)
		}
	}
}

func TestECSLaunchTemplateListPlainTargetUsesNameLookup(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{
		fakeLaunchTemplateListResponse("lt-1", "web-lt", 1),
	}}
	runCLI := catalogCaller(t, "ecs", "launch-template", fake)

	stdout, stderr, code := runCLI("ecs", "launch-template", "list", "--region", "cn-hangzhou", "web-lt")
	if code != 0 {
		t.Fatalf("ecs launch-template list by name exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "DescribeLaunchTemplates" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	if fake.calls[0].request["LaunchTemplateName.1"] != "web-lt" || fake.calls[0].request["LaunchTemplateId.1"] != nil {
		t.Fatalf("DescribeLaunchTemplates name lookup request = %#v", fake.calls[0].request)
	}
}

func TestECSLaunchTemplateListPrefixedTargetFallsBackToNameWhenIDMissing(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{
		{"RequestId": "req-missing-id", "TotalCount": 0, "LaunchTemplateSets": map[string]any{"LaunchTemplateSet": []any{}}},
		fakeLaunchTemplateListResponse("lt-1", "lt-web", 1),
	}}
	runCLI := catalogCaller(t, "ecs", "launch-template", fake)

	stdout, stderr, code := runCLI("ecs", "launch-template", "list", "--region", "cn-hangzhou", "lt-web")
	if code != 0 {
		t.Fatalf("ecs launch-template list fallback name exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 2 ||
		fake.calls[0].operation != "DescribeLaunchTemplates" ||
		fake.calls[1].operation != "DescribeLaunchTemplates" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	if fake.calls[0].request["LaunchTemplateId.1"] != "lt-web" {
		t.Fatalf("first lookup should use id for lt- target: %#v", fake.calls[0].request)
	}
	if fake.calls[1].request["LaunchTemplateName.1"] != "lt-web" {
		t.Fatalf("fallback lookup should use name for lt- target: %#v", fake.calls[1].request)
	}
}

func TestECSLaunchTemplateListUsesGenericResourceGroupAndTagFilters(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{
		fakeLaunchTemplateListResponse("lt-1", "web-lt", 1),
	}}
	runCLI := catalogCaller(t, "ecs", "launch-template", fake)

	stdout, stderr, code := runCLI(
		"ecs", "launch-template", "list",
		"--region", "cn-hangzhou",
		"--filter", "resource-group=rg-template",
		"--filter", "tag.env=prod",
	)
	if code != 0 {
		t.Fatalf("ecs launch-template list filters exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	req := fake.calls[0].request
	if req["TemplateResourceGroupId"] != "rg-template" || !sliceContainsValues(req["TemplateTag"], "env=prod") {
		t.Fatalf("DescribeLaunchTemplates filter request = %#v", req)
	}
}

func TestECSLaunchTemplateUpdateByNameReadsBackByName(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{
		{"RequestId": "req-default-version"},
		fakeLaunchTemplateVersionsResponse("lt-1", "web-lt", 2),
		fakeLaunchTemplateListResponse("lt-1", "web-lt", 1),
		fakeLaunchTemplateVersionsResponse("lt-1", "web-lt", 2),
	}}
	runCLI := catalogCaller(t, "ecs", "launch-template", fake)

	stdout, stderr, code := runCLI("ecs", "launch-template", "update", "--region", "cn-hangzhou", "--name", "web-lt", "--default-version", "2")
	if code != 0 {
		t.Fatalf("ecs launch-template update --name --default-version exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 4 ||
		fake.calls[0].operation != "ModifyLaunchTemplateDefaultVersion" ||
		fake.calls[1].operation != "DescribeLaunchTemplateVersions" ||
		fake.calls[2].operation != "DescribeLaunchTemplates" ||
		fake.calls[3].operation != "DescribeLaunchTemplateVersions" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	if fake.calls[1].request["LaunchTemplateName"] != "web-lt" || fake.calls[1].request["DefaultVersion"] != true {
		t.Fatalf("DescribeLaunchTemplateVersions name waiter request: %#v", fake.calls[1].request)
	}
	if fake.calls[2].request["LaunchTemplateName.1"] != "web-lt" {
		t.Fatalf("DescribeLaunchTemplates name readback request: %#v", fake.calls[2].request)
	}
	if fake.calls[3].request["LaunchTemplateName"] != "web-lt" || fake.calls[3].request["DefaultVersion"] != true {
		t.Fatalf("DescribeLaunchTemplateVersions name readback request: %#v", fake.calls[3].request)
	}
}

func TestECSLaunchTemplateUpdateUsesRenamedResourceFlags(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{
		{"RequestId": "req-create-version", "LaunchTemplateId": "lt-1", "LaunchTemplateVersionNumber": int64(2)},
	}}
	runCLI := catalogCaller(t, "ecs", "launch-template", fake)

	stdout, stderr, code := runCLI(
		"ecs", "launch-template", "update", "lt-1",
		"--region", "cn-hangzhou",
		"--create-version",
		"--resource-resource-group", "rg-instance",
		"--resource-tag", "env=prod",
		"--no-wait",
	)
	if code != 0 {
		t.Fatalf("ecs launch-template update renamed flags exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	req := fake.calls[0].request
	if req["ResourceGroupId"] != "rg-instance" || !sliceContainsValues(req["Tag"], "env=prod") {
		t.Fatalf("CreateLaunchTemplateVersion renamed flag request = %#v", req)
	}
}

func TestECSLaunchTemplateUpdateHelpUsesRenamedResourceFlags(t *testing.T) {
	t.Parallel()

	stdout, stderr, code := runCLI("--lang", "en", "ecs", "launch-template", "update", "--help")
	if code != 0 {
		t.Fatalf("ecs launch-template update --help exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	for _, want := range []string{"--resource-resource-group", "--resource-tag"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("update help missing %s: %s", want, stdout)
		}
	}
	for _, forbidden := range []string{"--template-resource-group", "--template-tag", "--tag"} {
		if strings.Contains(stdout, forbidden) {
			t.Fatalf("update help should not expose %s: %s", forbidden, stdout)
		}
	}
}

func TestECSLaunchTemplateUpdateDefaultVersionForwardsAPIParam(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{{"RequestId": "req-default-version"}}}
	runCLI := catalogCaller(t, "ecs", "launch-template", fake)

	stdout, stderr, code := runCLI(
		"ecs", "launch-template", "update", "lt-1",
		"--region", "cn-hangzhou",
		"--default-version", "2",
		"--api-param", "OwnerAccount=acct",
		"--no-wait",
	)
	if code != 0 {
		t.Fatalf("ecs launch-template update --default-version --api-param exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "ModifyLaunchTemplateDefaultVersion" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	if fake.calls[0].request["OwnerAccount"] != "acct" {
		t.Fatalf("ModifyLaunchTemplateDefaultVersion api_param request = %#v", fake.calls[0].request)
	}
}

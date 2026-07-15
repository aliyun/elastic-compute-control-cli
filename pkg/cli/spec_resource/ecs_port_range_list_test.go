package spec_resource

import (
	"strings"
	"testing"
)

func fakePortRangeListResponse(id string, entries []any) map[string]any {
	return map[string]any{
		"RequestId": "req-list",
		"PortRangeLists": []any{
			map[string]any{
				"PortRangeListId":   id,
				"PortRangeListName": "web-ports",
				"Description":       "web service ports",
				"MaxEntries":        20,
				"ResourceGroupId":   "rg-123",
				"AssociationCount":  1,
			},
		},
		"Entries": entries,
	}
}

func fakePortRangeListEntriesResponse(entries []any) map[string]any {
	return map[string]any{
		"RequestId": "req-entries",
		"Entries":   entries,
	}
}

func fakePortRangeListAssociationsResponse() map[string]any {
	return map[string]any{
		"RequestId": "req-associations",
		"PortRangeListAssociations": []any{
			map[string]any{
				"ResourceId":   "sg-123",
				"ResourceType": "SecurityGroup",
			},
		},
	}
}

func TestECSPortRangeListSchemaRegistersLifecycleCommands(t *testing.T) {
	t.Parallel()
	stdout, stderr, code := runCLI("--lang", "en", "schema", "ecs.port-range-list.create")
	if code != 0 {
		t.Fatalf("schema ecs.port-range-list.create exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	params, _ := decodeObject(t, stdout)["params"].(map[string]any)
	for _, name := range []string{"name", "description", "max-entries", "entry", "resource-group", "tag", "api-param"} {
		if _, ok := params[name]; !ok {
			t.Fatalf("create schema missing %q: %s", name, stdout)
		}
	}
	entry, _ := params["entry"].(map[string]any)
	if entry == nil || entry["type"] != "object" || entry["repeatable"] != true || entry["input"] != "inline-key-value|json|@file" {
		t.Fatalf("entry schema = %#v; stdout=%s", entry, stdout)
	}

	stdout, stderr, code = runCLI("--lang", "en", "schema", "ecs.port-range-list.get")
	if code != 0 {
		t.Fatalf("schema ecs.port-range-list.get exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	params, _ = decodeObject(t, stdout)["params"].(map[string]any)
	for _, name := range []string{"with-entries", "with-associations"} {
		if _, ok := params[name]; !ok {
			t.Fatalf("get schema missing %q: %s", name, stdout)
		}
	}

	stdout, stderr, code = runCLI("--lang", "en", "ecs", "prl", "get", "--help")
	if code != 0 {
		t.Fatalf("ecs prl get --help exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if strings.Contains(stdout, "--next-token") {
		t.Fatalf("port-range-list get help should not expose --next-token: %s", stdout)
	}

	stdout, stderr, code = runCLI("--lang", "en", "ecs", "prl", "list", "--help")
	if code != 0 {
		t.Fatalf("ecs prl list --help exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if strings.Contains(stdout, "--id string") || !strings.Contains(stdout, "--ids string") || !strings.Contains(stdout, "[ids...]") {
		t.Fatalf("port-range-list list help should use plural IDs: %s", stdout)
	}
}

func TestECSPortRangeListCreateRoutesCreatePortRangeList(t *testing.T) {
	t.Parallel()
	entries := []any{
		map[string]any{"PortRange": "80/80", "Description": "http"},
	}
	fake := &fakeSpecCaller{responses: []map[string]any{
		{"RequestId": "req-create", "PortRangeListId": "prl-123"},
		fakePortRangeListEntriesResponse(entries),
		fakePortRangeListResponse("prl-123", nil),
		fakePortRangeListEntriesResponse(entries),
	}}
	runCLI := catalogCaller(t, "ecs", "port-range-list", fake)

	stdout, stderr, code := runCLI("ecs", "port-range-list", "create",
		"--region", "cn-hangzhou",
		"--name", "web-ports",
		"--description", "web service ports",
		"--max-entries", "20",
		"--entry", "port-range=80/80,description=http",
		"--resource-group", "rg-123",
		"--tag", "env=prod",
	)
	if code != 0 {
		t.Fatalf("ecs port-range-list create exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 4 ||
		fake.calls[0].operation != "CreatePortRangeList" ||
		fake.calls[1].operation != "DescribePortRangeListEntries" ||
		fake.calls[2].operation != "DescribePortRangeLists" ||
		fake.calls[3].operation != "DescribePortRangeListEntries" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	request := fake.calls[0].request
	for key, want := range map[string]any{
		"RegionId":            "cn-hangzhou",
		"PortRangeListName":   "web-ports",
		"Description":         "web service ports",
		"MaxEntries":          20,
		"Entry.1.PortRange":   "80/80",
		"Entry.1.Description": "http",
		"ResourceGroupId":     "rg-123",
	} {
		if got := request[key]; got != want {
			t.Fatalf("CreatePortRangeList request[%s] = %#v, want %#v; request=%#v", key, got, want, request)
		}
	}
	if !sliceContainsValues(request["Tag"], "env=prod") {
		t.Fatalf("CreatePortRangeList tag request = %#v, want [env=prod]; request=%#v", request["Tag"], request)
	}
	if _, ok := request["ClientToken"]; !ok {
		t.Fatalf("CreatePortRangeList must receive ClientToken: %#v", request)
	}
	if fake.calls[1].request["PortRangeListId"] != "prl-123" ||
		fake.calls[2].request["PortRangeListId.1"] != "prl-123" ||
		fake.calls[3].request["PortRangeListId"] != "prl-123" {
		t.Fatalf("create readback requests = %#v %#v %#v", fake.calls[1].request, fake.calls[2].request, fake.calls[3].request)
	}
	portRangeList, _ := decodeObject(t, stdout)["port_range_list"].(map[string]any)
	if portRangeList == nil || portRangeList["id"] != "prl-123" || portRangeList["name"] != "web-ports" {
		t.Fatalf("unexpected output: %s", stdout)
	}
	readEntries, _ := portRangeList["entries"].([]any)
	if len(readEntries) != 1 {
		t.Fatalf("entries = %#v; stdout=%s", portRangeList["entries"], stdout)
	}
}

func TestECSPortRangeListUpdateRoutesModifyAndReadsBackEntries(t *testing.T) {
	t.Parallel()
	entries := []any{
		map[string]any{"PortRange": "443/443", "Description": "https"},
	}
	fake := &fakeSpecCaller{responses: []map[string]any{
		{"RequestId": "req-modify"},
		fakePortRangeListEntriesResponse(entries),
		fakePortRangeListEntriesResponse(entries),
		fakePortRangeListResponse("prl-123", nil),
		fakePortRangeListEntriesResponse(entries),
	}}
	runCLI := catalogCaller(t, "ecs", "port-range-list", fake)

	stdout, stderr, code := runCLI("ecs", "port-range-list", "update", "prl-123",
		"--region", "cn-hangzhou",
		"--name", "web-ports",
		"--entry", "+443/443,description=https",
		"--entry", "-80/80",
	)
	if code != 0 {
		t.Fatalf("ecs port-range-list update exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 5 ||
		fake.calls[0].operation != "ModifyPortRangeList" ||
		fake.calls[1].operation != "DescribePortRangeListEntries" ||
		fake.calls[2].operation != "DescribePortRangeListEntries" ||
		fake.calls[3].operation != "DescribePortRangeLists" ||
		fake.calls[4].operation != "DescribePortRangeListEntries" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	request := fake.calls[0].request
	for key, want := range map[string]any{
		"PortRangeListId":         "prl-123",
		"PortRangeListName":       "web-ports",
		"AddEntry.1.PortRange":    "443/443",
		"AddEntry.1.Description":  "https",
		"RemoveEntry.1.PortRange": "80/80",
	} {
		if got := request[key]; got != want {
			t.Fatalf("ModifyPortRangeList request[%s] = %#v, want %#v; request=%#v", key, got, want, request)
		}
	}
	if fake.calls[1].request["PortRangeListId"] != "prl-123" ||
		fake.calls[2].request["PortRangeListId"] != "prl-123" ||
		fake.calls[3].request["PortRangeListId.1"] != "prl-123" ||
		fake.calls[4].request["PortRangeListId"] != "prl-123" {
		t.Fatalf("readback requests = %#v %#v %#v %#v", fake.calls[1].request, fake.calls[2].request, fake.calls[3].request, fake.calls[4].request)
	}
	portRangeList, _ := decodeObject(t, stdout)["port_range_list"].(map[string]any)
	readEntries, _ := portRangeList["entries"].([]any)
	if len(readEntries) != 1 {
		t.Fatalf("entries = %#v; stdout=%s", portRangeList["entries"], stdout)
	}
}

func TestECSPortRangeListUpdateHelpUsesUnifiedEntryFlag(t *testing.T) {
	t.Parallel()

	stdout, stderr, code := runCLI("--lang", "en", "ecs", "port-range-list", "update", "--help")
	if code != 0 {
		t.Fatalf("ecs port-range-list update --help exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if !strings.Contains(stdout, "--entry") {
		t.Fatalf("update help missing --entry: %s", stdout)
	}
	for _, forbidden := range []string{"--add-entry", "--remove-entry"} {
		if strings.Contains(stdout, forbidden) {
			t.Fatalf("update help should not expose %s: %s", forbidden, stdout)
		}
	}
}

func TestECSPortRangeListUpdateRemoveOnlyWaitsForRemovedEntry(t *testing.T) {
	t.Parallel()
	entries := []any{
		map[string]any{"PortRange": "443/443", "Description": "https"},
	}
	fake := &fakeSpecCaller{responses: []map[string]any{
		{"RequestId": "req-modify"},
		fakePortRangeListEntriesResponse(entries),
		fakePortRangeListResponse("prl-123", nil),
		fakePortRangeListEntriesResponse(entries),
	}}
	runCLI := catalogCaller(t, "ecs", "port-range-list", fake)

	stdout, stderr, code := runCLI("ecs", "port-range-list", "update", "prl-123",
		"--region", "cn-hangzhou",
		"--entry", "-80/80",
	)
	if code != 0 {
		t.Fatalf("ecs port-range-list update remove-only exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 4 ||
		fake.calls[0].operation != "ModifyPortRangeList" ||
		fake.calls[1].operation != "DescribePortRangeListEntries" ||
		fake.calls[2].operation != "DescribePortRangeLists" ||
		fake.calls[3].operation != "DescribePortRangeListEntries" {
		t.Fatalf("calls = %#v", fake.calls)
	}
}

func TestECSPortRangeListUpdateNoWaitSkipsReadback(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{{"RequestId": "req-modify"}}}
	runCLI := catalogCaller(t, "ecs", "port-range-list", fake)

	stdout, stderr, code := runCLI("ecs", "port-range-list", "update", "prl-123",
		"--region", "cn-hangzhou",
		"--description", "updated ports",
		"--no-wait",
	)
	if code != 0 {
		t.Fatalf("ecs port-range-list update --no-wait exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "ModifyPortRangeList" {
		t.Fatalf("calls = %#v", fake.calls)
	}
}

func TestECSPortRangeListDeleteRoutesDeletePortRangeList(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{{"RequestId": "req-delete"}}}
	runCLI := catalogCaller(t, "ecs", "port-range-list", fake)

	stdout, stderr, code := runCLI("ecs", "port-range-list", "delete", "prl-123", "--region", "cn-hangzhou")
	if code != 0 {
		t.Fatalf("ecs port-range-list delete exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "DeletePortRangeList" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	if fake.calls[0].request["PortRangeListId"] != "prl-123" {
		t.Fatalf("DeletePortRangeList request = %#v", fake.calls[0].request)
	}
	out := decodeObject(t, stdout)
	portRangeList, _ := out["port_range_list"].(map[string]any)
	if out["deleted"] != true || portRangeList == nil || portRangeList["id"] != "prl-123" {
		t.Fatalf("unexpected output: %s", stdout)
	}
}

func TestECSPortRangeListGetWithEntriesAndAssociationsRunsExtraProbes(t *testing.T) {
	t.Parallel()
	entries := []any{
		map[string]any{"PortRange": "80/80", "Description": "http"},
	}
	fake := &fakeSpecCaller{responses: []map[string]any{
		fakePortRangeListResponse("prl-123", nil),
		fakePortRangeListAssociationsResponse(),
		fakePortRangeListEntriesResponse(entries),
	}}
	runCLI := catalogCaller(t, "ecs", "port-range-list", fake)

	stdout, stderr, code := runCLI("ecs", "port-range-list", "get", "prl-123",
		"--region", "cn-hangzhou",
		"--with-associations",
		"--with-entries",
	)
	if code != 0 {
		t.Fatalf("ecs port-range-list get exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 3 ||
		fake.calls[0].operation != "DescribePortRangeLists" ||
		fake.calls[1].operation != "DescribePortRangeListAssociations" ||
		fake.calls[2].operation != "DescribePortRangeListEntries" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	if fake.calls[0].request["PortRangeListId.1"] != "prl-123" ||
		fake.calls[1].request["PortRangeListId"] != "prl-123" ||
		fake.calls[2].request["PortRangeListId"] != "prl-123" {
		t.Fatalf("probe requests = %#v", fake.calls)
	}
	portRangeList, _ := decodeObject(t, stdout)["port_range_list"].(map[string]any)
	associations, _ := portRangeList["associations"].([]any)
	readEntries, _ := portRangeList["entries"].([]any)
	if len(associations) != 1 || len(readEntries) != 1 {
		t.Fatalf("unexpected related data: %#v stdout=%s", portRangeList, stdout)
	}
}

func TestECSPortRangeListAliasListRoutesDescribePortRangeLists(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{fakePortRangeListResponse("prl-123", nil)}}
	runCLI := catalogCaller(t, "ecs", "port-range-list", fake)

	stdout, stderr, code := runCLI("ecs", "prl", "list", "--region", "cn-hangzhou", "--limit", "50", "--next-token", "tok1")
	if code != 0 {
		t.Fatalf("ecs prl list exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "DescribePortRangeLists" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	if fake.calls[0].request["MaxResults"] != 50 || fake.calls[0].request["NextToken"] != "tok1" {
		t.Fatalf("DescribePortRangeLists pagination request = %#v", fake.calls[0].request)
	}
}

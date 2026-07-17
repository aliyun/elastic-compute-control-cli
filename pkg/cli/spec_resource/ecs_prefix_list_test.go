package spec_resource

import (
	"strings"
	"testing"
)

func fakePrefixListAttributeResponse(id string) map[string]any {
	return fakePrefixListAttributeResponseWithEntries(id, []any{
		map[string]any{"Cidr": "10.0.0.0/24", "Description": "web"},
	})
}

func fakePrefixListAttributeResponseWithEntries(id string, entries []any) map[string]any {
	return map[string]any{
		"RequestId":      "req-attr",
		"PrefixListId":   id,
		"PrefixListName": "web-pl",
		"AddressFamily":  "IPv4",
		"MaxEntries":     10,
		"Entries":        map[string]any{"Entry": entries},
	}
}

func fakePrefixListAssociationsResponse(resourceID string) map[string]any {
	return map[string]any{
		"RequestId": "req-associations",
		"PrefixListAssociations": map[string]any{"PrefixListAssociation": []any{
			map[string]any{"ResourceId": resourceID, "ResourceType": "SecurityGroup"},
		}},
	}
}

func TestECSPrefixListSchemaRegistration(t *testing.T) {
	t.Parallel()

	stdout, stderr, code := runCLI("--lang", "en", "schema", "ecs.prefix-list.create")
	if code != 0 {
		t.Fatalf("schema ecs.prefix-list.create exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if !strings.Contains(stdout, `"entry"`) || !strings.Contains(stdout, `"max-entries"`) {
		t.Fatalf("prefix-list create schema missing lifecycle entry fields: %s", stdout)
	}

	stdout, stderr, code = runCLI("--lang", "en", "ecs", "pl", "get", "--help")
	if code != 0 {
		t.Fatalf("ecs pl get --help exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if !strings.Contains(stdout, "--with-associations") {
		t.Fatalf("prefix-list alias help missing --with-associations: %s", stdout)
	}
	if strings.Contains(stdout, "--next-token") {
		t.Fatalf("prefix-list get help should not expose --next-token: %s", stdout)
	}

	stdout, stderr, code = runCLI("--lang", "en", "ecs", "pl", "list", "--help")
	if code != 0 {
		t.Fatalf("ecs pl list --help exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if strings.Contains(stdout, "--id string") || !strings.Contains(stdout, "--ids string") || !strings.Contains(stdout, "[ids...]") {
		t.Fatalf("prefix-list list help should use plural IDs: %s", stdout)
	}
}

func TestECSPrefixListCreateRoutesEntries(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{
		{"RequestId": "req-create", "PrefixListId": "pl-123"},
		fakePrefixListAttributeResponse("pl-123"),
		fakePrefixListAttributeResponse("pl-123"),
	}}
	runCLI := catalogCaller(t, "ecs", "prefix-list", fake)

	stdout, stderr, code := runCLI(
		"ecs", "prefix-list", "create",
		"--region", "cn-hangzhou",
		"--name", "web-pl",
		"--address-family", "IPv4",
		"--max-entries", "10",
		"--entry", "cidr=10.0.0.0/24,description=web",
	)
	if code != 0 {
		t.Fatalf("ecs prefix-list create exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 3 ||
		fake.calls[0].operation != "CreatePrefixList" ||
		fake.calls[1].operation != "DescribePrefixListAttributes" ||
		fake.calls[2].operation != "DescribePrefixListAttributes" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	req := fake.calls[0].request
	if req["PrefixListName"] != "web-pl" || req["AddressFamily"] != "IPv4" || req["MaxEntries"] != 10 {
		t.Fatalf("CreatePrefixList request = %#v", req)
	}
	if req["Entry.1.Cidr"] != "10.0.0.0/24" || req["Entry.1.Description"] != "web" {
		t.Fatalf("CreatePrefixList entry mapping wrong: %#v", req)
	}
}

func TestECSPrefixListUpdateRoutesAddAndRemoveEntries(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{
		{"RequestId": "req-modify"},
		fakePrefixListAttributeResponseWithEntries("pl-123", []any{
			map[string]any{"Cidr": "10.0.1.0/24", "Description": "app"},
		}),
		fakePrefixListAttributeResponseWithEntries("pl-123", []any{
			map[string]any{"Cidr": "10.0.1.0/24", "Description": "app"},
		}),
		fakePrefixListAttributeResponseWithEntries("pl-123", []any{
			map[string]any{"Cidr": "10.0.1.0/24", "Description": "app"},
		}),
	}}
	runCLI := catalogCaller(t, "ecs", "prefix-list", fake)

	stdout, stderr, code := runCLI(
		"ecs", "prefix-list", "update", "pl-123",
		"--region", "cn-hangzhou",
		"--name", "web-pl-2",
		"--entry", "+cidr=10.0.1.0/24,description=app",
		"--entry", "-cidr=10.0.0.0/24",
	)
	if code != 0 {
		t.Fatalf("ecs prefix-list update exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 4 ||
		fake.calls[0].operation != "ModifyPrefixList" ||
		fake.calls[1].operation != "DescribePrefixListAttributes" ||
		fake.calls[2].operation != "DescribePrefixListAttributes" ||
		fake.calls[3].operation != "DescribePrefixListAttributes" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	req := fake.calls[0].request
	if req["PrefixListId"] != "pl-123" || req["PrefixListName"] != "web-pl-2" {
		t.Fatalf("ModifyPrefixList request = %#v", req)
	}
	if req["AddEntry.1.Cidr"] != "10.0.1.0/24" || req["AddEntry.1.Description"] != "app" || req["RemoveEntry.1.Cidr"] != "10.0.0.0/24" {
		t.Fatalf("ModifyPrefixList entry mapping wrong: %#v", req)
	}
}

func TestECSPrefixListUpdateHelpUsesUnifiedEntryFlag(t *testing.T) {
	t.Parallel()

	stdout, stderr, code := runCLI("--lang", "en", "ecs", "prefix-list", "update", "--help")
	if code != 0 {
		t.Fatalf("ecs prefix-list update --help exit %d stderr=%s stdout=%s", code, stderr, stdout)
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

func TestECSPrefixListUpdateRemoveOnlyWaitsForRemovedEntry(t *testing.T) {
	t.Parallel()
	entries := []any{
		map[string]any{"Cidr": "10.0.1.0/24", "Description": "app"},
	}
	fake := &fakeSpecCaller{responses: []map[string]any{
		{"RequestId": "req-modify"},
		fakePrefixListAttributeResponseWithEntries("pl-123", entries),
		fakePrefixListAttributeResponseWithEntries("pl-123", entries),
	}}
	runCLI := catalogCaller(t, "ecs", "prefix-list", fake)

	stdout, stderr, code := runCLI(
		"ecs", "prefix-list", "update", "pl-123",
		"--region", "cn-hangzhou",
		"--entry", "-cidr=10.0.0.0/24",
	)
	if code != 0 {
		t.Fatalf("ecs prefix-list update remove-only exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 3 ||
		fake.calls[0].operation != "ModifyPrefixList" ||
		fake.calls[1].operation != "DescribePrefixListAttributes" ||
		fake.calls[2].operation != "DescribePrefixListAttributes" {
		t.Fatalf("calls = %#v", fake.calls)
	}
}

func TestECSPrefixListDeleteRoutesDeletePrefixList(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{{"RequestId": "req-delete"}}}
	runCLI := catalogCaller(t, "ecs", "prefix-list", fake)

	stdout, stderr, code := runCLI("ecs", "prefix-list", "delete", "pl-123", "--region", "cn-hangzhou")
	if code != 0 {
		t.Fatalf("ecs prefix-list delete exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "DeletePrefixList" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	if fake.calls[0].request["PrefixListId"] != "pl-123" {
		t.Fatalf("DeletePrefixList request = %#v", fake.calls[0].request)
	}
}

func TestECSPrefixListListRoutesDescribePrefixLists(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{{
		"RequestId": "req-list",
		"NextToken": "tok2",
		"PrefixLists": map[string]any{"PrefixList": []any{
			map[string]any{"PrefixListId": "pl-123", "PrefixListName": "web-pl", "AddressFamily": "IPv4"},
		}},
	}}}
	runCLI := catalogCaller(t, "ecs", "prefix-list", fake)

	stdout, stderr, code := runCLI("ecs", "pl", "list", "pl-123", "--region", "cn-hangzhou", "--limit", "50", "--next-token", "tok1", "--filter", "name=web-pl")
	if code != 0 {
		t.Fatalf("ecs pl list exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "DescribePrefixLists" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	req := fake.calls[0].request
	if req["MaxResults"] != 50 || req["NextToken"] != "tok1" || req["PrefixListName"] != "web-pl" || req["PrefixListId.1"] != "pl-123" {
		t.Fatalf("DescribePrefixLists request = %#v", req)
	}
	lists, _ := decodeObject(t, stdout)["prefix_lists"].([]any)
	if len(lists) != 1 {
		t.Fatalf("prefix_lists = %#v; stdout=%s", lists, stdout)
	}
}

func TestECSPrefixListGetWithAssociationsAddsProbe(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{
		fakePrefixListAttributeResponse("pl-123"),
		fakePrefixListAssociationsResponse("sg-123"),
	}}
	runCLI := catalogCaller(t, "ecs", "prefix-list", fake)

	stdout, stderr, code := runCLI("ecs", "prefix-list", "get", "pl-123", "--region", "cn-hangzhou", "--with-associations")
	if code != 0 {
		t.Fatalf("ecs prefix-list get --with-associations exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 2 || fake.calls[0].operation != "DescribePrefixListAttributes" || fake.calls[1].operation != "DescribePrefixListAssociations" {
		t.Fatalf("calls = %#v", fake.calls)
	}
	if fake.calls[1].request["PrefixListId"] != "pl-123" {
		t.Fatalf("DescribePrefixListAssociations request = %#v", fake.calls[1].request)
	}
	pl, _ := decodeObject(t, stdout)["prefix_list"].(map[string]any)
	associations, _ := pl["associations"].([]any)
	if len(associations) != 1 {
		t.Fatalf("associations = %#v; stdout=%s", pl["associations"], stdout)
	}
}

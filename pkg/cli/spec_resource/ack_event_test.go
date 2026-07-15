package spec_resource

import (
	"strings"
	"testing"

	"ecctl/pkg/engine"
	"ecctl/pkg/spec"
)

func ackEventCaller(t *testing.T, fake *fakeSpecCaller) func(args ...string) (string, string, int) {
	t.Helper()
	return withCaller(func(_ string, _ string, resource spec.ResourceSpec, region string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "ack" || resource.Resource != "event" || resource.APIProduct != "CS" {
			t.Fatalf("resource = %s/%s api=%s, want ack/event api=CS", resource.Product, resource.Resource, resource.APIProduct)
		}
		if region != "cn-hangzhou" {
			t.Fatalf("region = %q, want cn-hangzhou", region)
		}
		return fake, nil
	})
}

func ackEventResponse(requestID string, total int, eventID string) map[string]any {
	return map[string]any{
		"RequestId": requestID,
		"events": []any{
			map[string]any{
				"event_id":   eventID,
				"type":       "nodepool_upgrade",
				"source":     "task",
				"subject":    "np-123",
				"time":       "2025-05-14T10:00:56+08:00",
				"cluster_id": "c-123",
				"data": map[string]any{
					"level":   "info",
					"reason":  "Started",
					"message": "Start to upgrade NodePool nodePool/np-123",
				},
			},
		},
		"page_info": map[string]any{
			"page_size":   100,
			"page_number": 1,
			"total_count": total,
		},
	}
}

func TestACKEventHelpIsListOnlyAndShowsRoutingFlags(t *testing.T) {
	t.Parallel()

	stdout, stderr, code := runCLI("--lang", "en", "ack", "event", "--help")
	if code != 0 {
		t.Fatalf("ack event --help exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if !strings.Contains(stdout, "list") {
		t.Fatalf("ack event help missing list action:\n%s", stdout)
	}
	for _, unexpected := range []string{"create", "update", "delete"} {
		if strings.Contains(stdout, unexpected) {
			t.Fatalf("ack event help should be list-only, found %q:\n%s", unexpected, stdout)
		}
	}

	stdout, stderr, code = runCLI("--lang", "en", "ack", "event", "list", "--help")
	if code != 0 {
		t.Fatalf("ack event list --help exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	for _, want := range []string{"--cluster string", "--by-region", "--type string", "--source string", "--limit int", "--page int"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("ack event list help missing %q:\n%s", want, stdout)
		}
	}
}

func TestACKEventListRoutesDefaultClusterAPI(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{
		ackEventResponse("req-cluster", 1, "e-cluster"),
	}}
	runCLI := ackEventCaller(t, fake)

	stdout, stderr, code := runCLI("ack", "event", "list",
		"--region", "cn-hangzhou",
		"--cluster", "c-123",
	)
	if code != 0 {
		t.Fatalf("ack event list exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "DescribeClusterEvents" {
		t.Fatalf("calls = %#v, want DescribeClusterEvents", fake.calls)
	}
	request := fake.calls[0].request
	want := map[string]any{
		"ClusterId":   "c-123",
		"page_size":   100,
		"page_number": 1,
	}
	for key, value := range want {
		if request[key] != value {
			t.Fatalf("%s = %#v, want %#v; request=%#v", key, request[key], value, request)
		}
	}

	out := decodeObject(t, stdout)
	events, _ := out["events"].([]any)
	if len(events) != 1 {
		t.Fatalf("events len = %d, want 1; stdout=%s", len(events), stdout)
	}
	event, _ := events[0].(map[string]any)
	if event["id"] != "e-cluster" || event["cluster_id"] != "c-123" {
		t.Fatalf("event = %#v; stdout=%s", event, stdout)
	}
	pagination, _ := out["pagination"].(map[string]any)
	if pagination["page"] != float64(1) || pagination["limit"] != float64(100) || pagination["has_more"] != false {
		t.Fatalf("pagination = %#v; stdout=%s", pagination, stdout)
	}
}

func TestACKEventListRequiresARouteSelector(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{}
	runCLI := ackEventCaller(t, fake)

	stdout, stderr, code := runCLI("ack", "event", "list",
		"--region", "cn-hangzhou",
	)
	if code == 0 {
		t.Fatalf("ack event list without route selector succeeded stdout=%s stderr=%s", stdout, stderr)
	}
	if len(fake.calls) != 0 {
		t.Fatalf("missing route selector should fail before API call: %#v", fake.calls)
	}
	message := errorMessage(t, stdout)
	for _, want := range []string{"--cluster", "--by-region", "--type", "--source"} {
		if !strings.Contains(message, want) {
			t.Fatalf("error missing %q: %s", want, message)
		}
	}
}

func TestACKEventListByRegionRoutesRegionalAPI(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{
		ackEventResponse("req-region", 3, "e-region"),
	}}
	runCLI := ackEventCaller(t, fake)

	stdout, stderr, code := runCLI("ack", "event", "list",
		"--region", "cn-hangzhou",
		"--by-region",
		"--limit", "20",
		"--page", "2",
	)
	if code != 0 {
		t.Fatalf("ack event list --by-region exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "DescribeEventsForRegion" {
		t.Fatalf("calls = %#v, want DescribeEventsForRegion", fake.calls)
	}
	request := fake.calls[0].request
	want := map[string]any{
		"region_id":   "cn-hangzhou",
		"page_size":   20,
		"page_number": 2,
	}
	for key, value := range want {
		if request[key] != value {
			t.Fatalf("%s = %#v, want %#v; request=%#v", key, request[key], value, request)
		}
	}
	if _, ok := request["ClusterId"]; ok {
		t.Fatalf("by-region request must not use ClusterId: %#v", request)
	}
	if _, ok := request["cluster_id"]; ok {
		t.Fatalf("by-region request must not use cluster_id: %#v", request)
	}
}

func TestACKEventListTypeAndSourceRouteAggregateAPI(t *testing.T) {
	t.Parallel()
	fake := &fakeSpecCaller{responses: []map[string]any{
		ackEventResponse("req-events", 1, "e-events"),
	}}
	runCLI := ackEventCaller(t, fake)

	stdout, stderr, code := runCLI("ack", "event", "list",
		"--region", "cn-hangzhou",
		"--cluster", "c-123",
		"--type", "nodepool_upgrade",
		"--source", "task",
	)
	if code != 0 {
		t.Fatalf("ack event list --type --source exit %d stderr=%s stdout=%s", code, stderr, stdout)
	}
	if len(fake.calls) != 1 || fake.calls[0].operation != "DescribeEvents" {
		t.Fatalf("calls = %#v, want DescribeEvents", fake.calls)
	}
	request := fake.calls[0].request
	want := map[string]any{
		"cluster_id":  "c-123",
		"type":        "nodepool_upgrade",
		"source":      "task",
		"page_size":   100,
		"page_number": 1,
	}
	for key, value := range want {
		if request[key] != value {
			t.Fatalf("%s = %#v, want %#v; request=%#v", key, request[key], value, request)
		}
	}
	if _, ok := request["ClusterId"]; ok {
		t.Fatalf("DescribeEvents request must use cluster_id, not ClusterId: %#v", request)
	}
}

package report

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

func TestAggregateKeepsExecutionRegionMappingsOnCases(t *testing.T) {
	start := time.Now()
	run := Aggregate("run-1", "public", "ecctl", []Execution{
		{
			ID: "execution-01", Signature: "primary=[ecs.image]",
			Regions:   map[string]string{"primary": "cn-hangzhou"},
			Attempts:  []ExecutionAttempt{{Regions: map[string]string{"primary": "cn-hangzhou"}, Status: "pass"}},
			StartedAt: start, FinishedAt: start.Add(time.Second), Parameters: map[string]any{"ecs": map[string]any{"zone": "cn-hangzhou-b"}},
			Cases: []Case{{Name: "image", Resource: "ecs/image", Status: StatusPass}},
		},
		{
			ID: "execution-02", Signature: "primary=[ecs.image];destination=[ecs.image]!=primary",
			Regions:   map[string]string{"primary": "cn-hangzhou", "destination": "cn-zhangjiakou"},
			Attempts:  []ExecutionAttempt{{Regions: map[string]string{"primary": "cn-hangzhou", "destination": "cn-zhangjiakou"}, Status: "fail"}},
			StartedAt: start.Add(time.Second), FinishedAt: start.Add(2 * time.Second),
			Cases: []Case{{Name: "image-copy", Resource: "ecs/image", Status: StatusFail}},
		},
	})

	if run.Region != "" || run.RegionAttempts != nil || run.Parameters != nil {
		t.Fatalf("multi-execution report populated legacy fields: %+v", run)
	}
	if run.Summary.Cases != 2 || run.Summary.Passed != 1 || run.Summary.Failed != 1 {
		t.Fatalf("summary = %+v", run.Summary)
	}
	if got := run.Cases[1].ExecutionID; got != "execution-02" {
		t.Fatalf("case execution id = %q", got)
	}
	if got := run.Executions[1].Cases[0].ExecutionID; got != "execution-02" {
		t.Fatalf("nested case execution id = %q", got)
	}
	wantRegions := map[string]string{"primary": "cn-hangzhou", "destination": "cn-zhangjiakou"}
	if !reflect.DeepEqual(run.Cases[1].Regions, wantRegions) {
		t.Fatalf("case regions = %#v", run.Cases[1].Regions)
	}
	data, err := json.Marshal(run)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatal(err)
	}
	if got := payload["schema_version"]; got != float64(2) {
		t.Fatalf("schema_version = %#v, want 2", got)
	}
}

func TestAggregatePopulatesLegacyFieldsForSingleExecution(t *testing.T) {
	parameters := map[string]any{"ecs": map[string]any{"zone": "cn-hangzhou-b"}}
	run := Aggregate("run-1", "public", "ecctl", []Execution{{
		ID: "execution-01", Regions: map[string]string{"primary": "cn-zhangjiakou"}, Parameters: parameters,
		Attempts: []ExecutionAttempt{
			{Regions: map[string]string{"primary": "cn-hangzhou"}, Status: "fail", Reason: "unavailable"},
			{Regions: map[string]string{"primary": "cn-zhangjiakou"}, Status: "pass"},
		},
		Cases: []Case{{Name: "region", Resource: "ecs/region", Status: StatusPass}},
	}})

	if run.Region != "cn-zhangjiakou" {
		t.Fatalf("legacy region = %q", run.Region)
	}
	if len(run.RegionAttempts) != 2 || run.RegionAttempts[1].Region != "cn-zhangjiakou" {
		t.Fatalf("legacy attempts = %#v", run.RegionAttempts)
	}
	if !reflect.DeepEqual(run.Parameters, parameters) {
		t.Fatalf("legacy parameters = %#v", run.Parameters)
	}
}

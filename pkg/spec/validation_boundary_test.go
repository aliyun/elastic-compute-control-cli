package spec

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func validationFixture() ResourceSpec {
	return ResourceSpec{SchemaVersion: 2, Product: "fixture", Resource: "thing", Kind: "regional",
		Schema:   ResourceSchema{Fields: map[string]SchemaField{"id": {Type: "string"}}},
		Probes:   map[string]Probe{"detail": {API: "GetThing"}},
		Bindings: map[string]Binding{}, Operations: map[string]Operation{}, Waiters: map[string]Waiter{}}
}

func TestValidateRejectsMalformedWaiterAndResponseContracts(t *testing.T) {
	for _, tt := range []struct {
		name, want string
		waiter     Waiter
	}{
		{"capture needs identity", "match by", Waiter{Match: WaiterMatch{Capture: "items"}}},
		{"per item needs capture", "requires capture", Waiter{Match: WaiterMatch{ProbeEachCapture: true}}},
		{"mixed matchers", "cannot be combined", Waiter{Match: WaiterMatch{Capture: "items", By: []string{"id"}, Fields: map[string]string{"id": "$.id"}}}},
		{"empty match field", "non-empty field", Waiter{Match: WaiterMatch{Fields: map[string]string{"": "$.id"}}}},
		{"empty contains expression", "non-empty field", Waiter{Match: WaiterMatch{Contains: map[string]string{"id": ""}}}},
		{"empty excludes expression", "non-empty field", Waiter{Match: WaiterMatch{Excludes: map[string]string{"id": " "}}}},
		{"pending field", "field is required", Waiter{Pending: []WaiterPending{{Values: []string{"pending"}}}}},
		{"pending values", "values are required", Waiter{Pending: []WaiterPending{{Field: "state"}}}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := validationFixture()
			tt.waiter.Probe, tt.waiter.Target = "detail", "present"
			r.Waiters["ready"] = tt.waiter
			if err := Validate(r); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("invalid waiter = %v, want %q", err, tt.want)
			}
		})
	}
	for _, tt := range []struct {
		name, want string
		mutate     func(*BindingResponse)
	}{
		{"missing status", "status is required", func(r *BindingResponse) { r.Status = "" }},
		{"bad status", "status path", func(r *BindingResponse) { r.Status = "status" }},
		{"missing success", "success is required", func(r *BindingResponse) { r.Success = nil }},
		{"missing code", "error_code is required", func(r *BindingResponse) { r.ErrorCode = "" }},
		{"bad code", "error_code path", func(r *BindingResponse) { r.ErrorCode = "code" }},
		{"missing message", "error_message is required", func(r *BindingResponse) { r.ErrorMessage = "" }},
		{"bad message", "error_message path", func(r *BindingResponse) { r.ErrorMessage = "message" }},
		{"bad request id", "request_id path", func(r *BindingResponse) { r.RequestID = "requestId" }},
		{"capture needs match fields", "match by is required", func(r *BindingResponse) { r.Match = BindingResponseMatch{Capture: "items"} }},
		{"match needs capture", "match capture is required", func(r *BindingResponse) { r.Match = BindingResponseMatch{By: []string{"id"}} }},
		{"validation needs items", "items is required", func(r *BindingResponse) { r.Items = "" }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := validationFixture()
			response := BindingResponse{Items: "$.items", Status: "$.status", Success: []string{"SUCCESS"}, ErrorCode: "$.code", ErrorMessage: "$.message"}
			tt.mutate(&response)
			r.Bindings["change"] = Binding{API: "Change", Request: map[string]any{}, Response: response}
			if err := Validate(r); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("invalid binding response = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestValidateRejectsMalformedOperationAndSchemaContracts(t *testing.T) {
	for _, tt := range []struct {
		name, want string
		mutate     func(*ResourceSpec)
	}{
		{"unknown kind", "kind must", func(r *ResourceSpec) { r.Kind = "unknown" }},
		{"control schema", "control", func(r *ResourceSpec) { r.Controls = map[string]SchemaField{"size": {Type: "unknown"}} }},
		{"extra field schema", "extra field", func(r *ResourceSpec) {
			r.Probes["detail"] = Probe{API: "GetThing", Response: ProbeResponse{ExtraFields: map[string]ProbeField{"field": {}}}}
		}},
		{"binding requirement", "require_all", func(r *ResourceSpec) {
			r.Bindings["change"] = Binding{API: "Change", Request: map[string]any{}, RequireAll: []Requirement{{}}}
		}},
		{"operation requirement", "require_any", func(r *ResourceSpec) { r.Operations["list"] = Operation{RequireAny: []Requirement{{}}} }},
		{"empty conflict", "conflicts", func(r *ResourceSpec) { r.Operations["list"] = Operation{Conflicts: []Conflict{{}}} }},
		{"unknown conflict input", "unknown input", func(r *ResourceSpec) {
			r.Operations["list"] = Operation{Conflicts: []Conflict{{Any: []string{"missing"}, WithAny: []string{"id"}}}}
		}},
		{"unknown waiter", "unknown waiter", func(r *ResourceSpec) { r.Operations["list"] = Operation{Workflow: []WorkflowStep{{Wait: "missing"}}} }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := validationFixture()
			tt.mutate(&r)
			if err := Validate(r); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("invalid spec = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestDefaultSpecDirectoryFindsModuleAncestor(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module fixture\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	child := filepath.Join(root, "nested", "child")
	if err := os.MkdirAll(child, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(child)
	want, err := DefaultDir()
	if err != nil {
		t.Fatal(err)
	}
	rootInfo, err := os.Stat(root)
	if err != nil {
		t.Fatal(err)
	}
	parentInfo, err := os.Stat(filepath.Dir(want))
	if err != nil || filepath.Base(want) != "specs" || !os.SameFile(rootInfo, parentInfo) {
		t.Fatalf("default directory = %q, %v", want, err)
	}
	if got := resolveSpecDir(""); got != want {
		t.Fatalf("resolved default = %q", got)
	}
	if err := os.Remove(filepath.Join(root, "go.mod")); err != nil {
		t.Fatal(err)
	}
	if _, err := DefaultDir(); err == nil {
		t.Fatal("missing module root accepted")
	}
	if got := resolveSpecDir(""); got != "" {
		t.Fatalf("missing module default = %q", got)
	}
}

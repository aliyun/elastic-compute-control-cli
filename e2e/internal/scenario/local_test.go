package scenario

import (
	"strings"
	"testing"
)

func TestSuiteValidatesConstrainedLocalActions(t *testing.T) {
	valid := Suite{Surface: SurfacePublic, Resource: "ecs/image", Execution: ExecutionDAG, Steps: []Step{{
		Name: "extract", Local: &LocalAction{Action: "extract-tar-gzip", Source: "{{.work_dir}}/export.tar.gz", Destination: "{{.work_dir}}/import.raw"},
	}}}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid local action: %v", err)
	}

	tests := []struct {
		name string
		step Step
		want string
	}{
		{name: "both", step: Step{Name: "extract", Run: "ecctl ecs image list", Local: &LocalAction{Action: "extract-tar-gzip", Source: "a", Destination: "b"}}, want: "exactly one"},
		{name: "unsupported", step: Step{Name: "extract", Local: &LocalAction{Action: "shell", Source: "a", Destination: "b"}}, want: "unsupported local action"},
		{name: "teardown", step: Step{Name: "extract", Local: &LocalAction{Action: "extract-tar-gzip", Source: "a", Destination: "b"}, Teardown: "ecctl ecs image delete m-1"}, want: "cannot declare teardown"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			suite := Suite{Surface: SurfacePublic, Resource: "ecs/image", Execution: ExecutionDAG, Steps: []Step{test.step}}
			err := suite.Validate()
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Validate error = %v, want %q", err, test.want)
			}
		})
	}
}

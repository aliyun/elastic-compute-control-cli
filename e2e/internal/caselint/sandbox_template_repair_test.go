package caselint

import (
	"testing"

	"github.com/aliyun/elastic-compute-control-cli/e2e/internal/scenario"
)

func TestSandboxLifecycleSuitesOwnUniqueResources(t *testing.T) {
	suites, err := scenario.LoadDir("../../cases/sandbox")
	if err != nil {
		t.Fatal(err)
	}
	report := &Report{}
	checkUniqueSuiteResources(report, suites)
	if len(report.Errors) != 0 {
		t.Fatalf("sandbox lifecycle identities violate lint contract: %+v", report.Errors)
	}
}

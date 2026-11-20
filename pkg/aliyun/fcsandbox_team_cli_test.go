package aliyun_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/aliyun/elastic-compute-control-cli/pkg/aliyun"
	"github.com/aliyun/elastic-compute-control-cli/pkg/cli"
	"github.com/aliyun/elastic-compute-control-cli/pkg/engine"
	"github.com/aliyun/elastic-compute-control-cli/pkg/spec"
)

func TestTeamEnriched404SDKErrorProducesJournalProof(t *testing.T) {
	caller := aliyun.TeamSDKErrorCallerForTest(t, 404, `{"code":"TeamNotFound","httpStatusCode":404,"message":"gone","requestId":"req-wire","success":false}`)
	dir := t.TempDir()
	t.Setenv("ECCTL_CONFIG_PATH", filepath.Join(dir, "missing-ecctl.json"))
	t.Setenv("ECCTL_ALIYUN_CONFIG_PATH", filepath.Join(dir, "missing-aliyun.json"))
	t.Setenv("ECCTL_PROFILE", "")
	ctx := cli.WithResourceCallerFactory(context.Background(), func(_, _ string, resource spec.ResourceSpec, region string, _ func(string) string) (engine.Caller, error) {
		if resource.Product != "sandbox" || resource.Resource != "team" || resource.APIProduct != "FCSandbox" || region != "cn-hangzhou" {
			t.Fatalf("Team command routed incorrectly: %+v region=%q", resource, region)
		}
		return caller, nil
	})
	var stdout, stderr bytes.Buffer
	exit := cli.Run(ctx, []string{"--lang", "en", "sandbox", "team", "get", "team-1", "--region", "cn-hangzhou", "--output=json", "--agent-envelope=false"}, &stdout, &stderr)
	if exit != 4 || stderr.Len() != 0 {
		t.Fatalf("enriched 404 CLI exit=%d stdout=%s stderr=%s", exit, stdout.String(), stderr.String())
	}
	var output, want map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatalf("invalid CLI JSON: %v", err)
	}
	fixture, err := os.ReadFile(filepath.Join("..", "..", "e2e", "fixtures", "team-enriched404-cli.json"))
	if err != nil {
		t.Fatalf("read journal contract fixture: %v; CLI output=%s", err, stdout.String())
	}
	if err := json.Unmarshal(fixture, &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(output, want) {
		t.Fatalf("CLI output differs from journal contract: got=%s want=%s", stdout.String(), fixture)
	}
}
